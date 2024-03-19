package ref_dump

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"bou.ke/monkey"
)

type Record struct {
	T     *_type
	Base  uintptr
	IsArr bool
	Dim   int
}

func _type2Type(p *_type) reflect.Type {
	typ := reflect.TypeOf(*(*interface{})(unsafe.Pointer(&p)))
	return typ
}

func (r Record) Type() reflect.Type {
	return _type2Type(r.T)
}

const allocRecCap = 1024 * 1024 * 1024 * 1

var (
	allocRecords []Record
	hookEnabled  bool
)

func addRec(k reflect.Kind, record Record) {
	if k < reflect.Array || !hookEnabled {
		return
	}
	if len(allocRecords) < cap(allocRecords) {
		allocRecords = append(allocRecords, record)
	} else {
		fmt.Fprintf(os.Stderr, "alloc buffer overflow")
	}
}

func newobject_p(tp *_type) unsafe.Pointer {
	t := _type2Type(tp)
	r := mallocgc(tp.size, tp, true)
	addRec(t.Kind(), Record{tp, uintptr(r), false, 1})
	return r
}

func newarray_p(tp *_type, n int) unsafe.Pointer {
	t := _type2Type(tp)
	r := mallocgc(tp.size*uintptr(n), tp, true)
	addRec(t.Kind(), Record{tp, uintptr(r), true, n})
	return r
}

func clobberfree_p(x unsafe.Pointer, size uintptr) {
	if !Opt.TraceFree {
		return
	}
	for _, r := range allocRecords {
		if r.Base == uintptr(x) {
			r.Base = 0
			break
		}
	}
}

func growslice_p(oldPtr unsafe.Pointer, newLen, oldCap, num int, et *_type) slice {
	oldLen := newLen - num
	if et.size == 0 {
		return slice{unsafe.Pointer(&zerobase), newLen, newLen}
	}

	newcap := oldCap
	doublecap := newcap + newcap
	if newLen > doublecap {
		newcap = newLen
	} else {
		const threshold = 256
		if oldCap < threshold {
			newcap = doublecap
		} else {
			for 0 < newcap && newcap < newLen {
				newcap += (newcap + 3*threshold) / 4
			}
			if newcap <= 0 {
				newcap = newLen
			}
		}
	}

	var lenmem, newlenmem, capmem uintptr
	lenmem = uintptr(oldLen) * et.size
	newlenmem = uintptr(newLen) * et.size
	capmem = et.size * uintptr(newcap)
	newcap = int(capmem / et.size)
	capmem = uintptr(newcap) * et.size

	var p unsafe.Pointer
	if et.ptrdata == 0 {
		p = mallocgc(capmem, nil, false)
		memclrNoHeapPointers(add(p, newlenmem), capmem-newlenmem)
	} else {
		p = mallocgc(capmem, et, true)
		addRec(_type2Type(et).Kind(), Record{et, uintptr(p), true, newcap})
		if lenmem > 0 && writeBarrier.enabled {
			bulkBarrierPreWriteSrcOnly(uintptr(p), uintptr(oldPtr), lenmem-et.size+et.ptrdata)
		}
	}
	memmove(p, oldPtr, lenmem)

	return slice{p, newLen, newcap}
}

func gogetenv_p(key string) string {
	if key == "GODEBUG" {
		return "clobberfree=1"
	}
	return ""
}

var Opt struct {
	FuncInfo   bool
	MaxAlloc   int
	ScanGlobal bool
	TraceFree  bool
	MaxDepth   int
}

func InitHooks() {
	EnableHook(false)
	defer EnableHook(true)

	if Opt.MaxAlloc == 0 {
		Opt.MaxAlloc = allocRecCap
	}
	allocRecords = make([]Record, 0, Opt.MaxAlloc)

	monkey.Patch(newobject, newobject_p)
	monkey.Patch(newarray, newarray_p)
	monkey.Patch(clobberfree, clobberfree_p)
	monkey.Patch(growslice, growslice_p)
	monkey.Patch(gogetenv, gogetenv_p)

	parsedebugvars()
}

type Node struct {
	typ      reflect.Type
	typeName string
	addr     uintptr
	arr      bool
	scanned  bool
	saved    bool
}

var closureReg = regexp.MustCompile("struct \\{ F uintptr;")

type closure struct {
	F uintptr
}

func (n *Node) TypeName() string {
	if len(n.typeName) > 0 {
		return n.typeName
	}

	name := n.typ.String()
	if Opt.FuncInfo && closureReg.MatchString(name) {
		pc := (*closure)(unsafe.Pointer(n.addr))
		f := runtime.FuncForPC(pc.F)
		if f != nil {
			file, line := f.FileLine(pc.F)
			name += fmt.Sprintf("\n(%s:%d)", file, line)
		}
	}
	if n.arr {
		n.typeName = "[]" + name
	} else {
		n.typeName = name
	}
	return n.typeName
}

type AllocDb struct {
	all  map[uintptr]*Node
	deps map[string]map[string]struct{}
}

func (db *AllocDb) addDep(tp string, dep string) {
	if _, ok := db.deps[tp]; !ok {
		db.deps[tp] = map[string]struct{}{}
	}
	db.deps[tp][dep] = struct{}{}
}

func findParent(n *Node, db *AllocDb, depth int) {
	if n.scanned || n.addr == 0 {
		return
	}
	if Opt.MaxDepth > 0 && depth > Opt.MaxDepth {
		return
	}

	base := n.addr
	var parents []*Node
	for _, r := range allocRecords {
		t := r.Type()
		for i := uintptr(0); i < t.Size()*uintptr(r.Dim); i += 8 {
			if *(*uintptr)(unsafe.Pointer(r.Base + i)) == base {
				if p := db.all[r.Base]; p != nil {
					parents = append(parents, p)
					db.addDep(p.TypeName(), n.TypeName())
				}
				break
			}
		}
	}

	if Opt.ScanGlobal {
		for _, datap := range activeModules() {
			for i := datap.data; i <= datap.edata; i += 8 {
				if *(*uintptr)(unsafe.Pointer(i)) == base {
					db.addDep("#GlobalDataSection", n.TypeName())
					break
				}
			}
			for i := datap.bss; i <= datap.ebss; i += 8 {
				if *(*uintptr)(unsafe.Pointer(i)) == base {
					db.addDep("#GlobalBssSection", n.TypeName())
					break
				}
			}
		}
	}
	n.scanned = true

	for _, p := range parents {
		findParent(p, db, depth+1)
	}
}

func randColor() string {
	return fmt.Sprintf("#%02x%02x%02x", rand.Int()%255, rand.Int()%255, rand.Int()%255)
}

func dumpNodeDeps(db *AllocDb, f *os.File) {
	for p, s := range db.deps {
		isGlobal := true
	globalCheck:
		for k, v := range db.deps {
			if k == p {
				continue
			}
			for j := range v {
				if j == p {
					isGlobal = false
					break globalCheck
				}
			}
		}

		for k := range s {
			if isGlobal {
				f.WriteString(fmt.Sprintf(
					"\"%s\" -> \"%s\"; \"%s\" [style=filled, fillcolor=red, color=\"%s\"];\n",
					p, k, p, randColor()))
			} else {
				f.WriteString(fmt.Sprintf("\"%s\" -> \"%s\" [color=\"%s\"];\n", p, k, randColor()))
			}
		}
	}
}

func EnableHook(v bool) bool {
	old := hookEnabled
	hookEnabled = v
	return old
}

func DumpRefsToDot(addr uintptr, outfile string) {
	old := EnableHook(false)
	defer EnableHook(old)

	db := &AllocDb{all: map[uintptr]*Node{}, deps: map[string]map[string]struct{}{}}
	for _, r := range allocRecords {
		db.all[r.Base] = &Node{addr: r.Base, typ: r.Type(), arr: r.IsArr}
	}
	n := db.all[addr]
	findParent(n, db, 0)

	file, _ := os.OpenFile(outfile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	defer file.Close()
	file.WriteString("digraph {\n")
	dumpNodeDeps(db, file)
	file.WriteString("}\n")
}

func HexToUintptr(addr string) uintptr {
	addr = strings.Replace(addr, "0x", "", 1)
	addr1, err := strconv.ParseInt(addr, 16, 64)
	if err != nil {
		return 0
	}

	return uintptr(addr1)
}

func DumpRefs(addr uintptr, outfile string) {
	old := EnableHook(false)
	defer EnableHook(old)

	if strings.HasSuffix(outfile, ".svg") {
		tmp := "tmp.dot"
		DumpRefsToDot(addr, tmp)
		defer os.Remove(tmp)
		c := exec.Command("dot", "-Tsvg", tmp, "-o", outfile)
		c.Run()
	} else {
		DumpRefsToDot(addr, outfile)
	}
}
