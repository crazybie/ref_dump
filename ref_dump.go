package ref_dump

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"bou.ke/monkey"
)

type _type struct {
	size    uintptr
	ptrdata uintptr
}

type slice struct {
	array unsafe.Pointer
	len   int
	cap   int
}

//go:linkname newobject runtime.newobject
func newobject(typ *_type) unsafe.Pointer

//go:linkname newarray runtime.newarray
func newarray(typ *_type, n int) unsafe.Pointer

//go:linkname parsedebugvars runtime.parsedebugvars
func parsedebugvars()

//go:linkname gogetenv runtime.gogetenv
func gogetenv(key string) string

//go:linkname mallocgc runtime.mallocgc
func mallocgc(size uintptr, typ *_type, needzero bool) unsafe.Pointer

//go:linkname clobberfree runtime.clobberfree
func clobberfree(x unsafe.Pointer, size uintptr)

//go:linkname growslice runtime.growslice
func growslice(oldPtr unsafe.Pointer, newLen, oldCap, num int, et *_type) slice

// base address for all 0-byte allocations
var zerobase uintptr

type writeBarrierT struct {
	enabled bool
}

//go:linkname writeBarrier runtime.writeBarrierT
var writeBarrier writeBarrierT

//go:linkname add runtime.add
func add(p unsafe.Pointer, x uintptr) unsafe.Pointer

//go:linkname memclrNoHeapPointers runtime.memclrNoHeapPointers
func memclrNoHeapPointers(ptr unsafe.Pointer, n uintptr)

//go:linkname bulkBarrierPreWriteSrcOnly runtime.bulkBarrierPreWriteSrcOnly
func bulkBarrierPreWriteSrcOnly(dst, src, size uintptr)

//go:linkname memmove runtime.memmove
func memmove(to, from unsafe.Pointer, n uintptr)

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
	hookEnabled  = true
)

func newobject_p(tp *_type) unsafe.Pointer {
	t := _type2Type(tp)
	r := mallocgc(tp.size, tp, true)
	if t.Kind() < reflect.Array || !hookEnabled {
		return r
	}
	if len(allocRecords) < cap(allocRecords) {
		allocRecords = append(allocRecords, Record{tp, uintptr(r), false, 1})
	}
	return r
}

func newarray_p(tp *_type, n int) unsafe.Pointer {
	t := _type2Type(tp)
	r := mallocgc(tp.size*uintptr(n), tp, true)
	if t.Kind() < reflect.Array || !hookEnabled {
		return r
	}
	if len(allocRecords) < cap(allocRecords) {
		allocRecords = append(allocRecords, Record{tp, uintptr(r), true, n})
	}
	return r
}

func clobberfree_p(x unsafe.Pointer, size uintptr) {
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
		if hookEnabled {
			allocRecords = append(allocRecords, Record{et, uintptr(p), true, newcap})
		}
		if lenmem > 0 && writeBarrier.enabled {
			bulkBarrierPreWriteSrcOnly(uintptr(p), uintptr(oldPtr), lenmem-et.size+et.ptrdata)
		}
	}
	memmove(p, oldPtr, lenmem)

	return slice{p, newLen, newcap}
}

func HookGc() {
	allocRecords = make([]Record, 0, allocRecCap)

	monkey.Patch(newobject, newobject_p)
	monkey.Patch(newarray, newarray_p)
	monkey.Patch(clobberfree, clobberfree_p)
	monkey.Patch(growslice, growslice_p)

	monkey.Patch(gogetenv, func(key string) string {
		if key == "GODEBUG" {
			return "clobberfree=1"
		}
		return ""
	})

	parsedebugvars()
}

type Node struct {
	typ     reflect.Type
	addr    uintptr
	arr     bool
	scanned bool
	saved   bool
}

func (n *Node) TypeName() string {
	name := n.typ.String()
	if n.arr {
		return "[]" + name
	}
	return name
}

type AllocDb struct {
	all  map[uintptr]*Node
	deps map[string]map[string]struct{}
}

func findParent(n *Node, db *AllocDb) {
	if n.scanned || n.addr == 0 {
		return
	}

	var parents []*Node
	for _, r := range allocRecords {
		t := r.Type()
		for i := uintptr(0); i < t.Size()*uintptr(r.Dim); i += 8 {
			if *(*uintptr)(unsafe.Pointer(r.Base + i)) == n.addr {
				if p := db.all[r.Base]; p != nil {
					parents = append(parents, p)

					if _, ok := db.deps[p.TypeName()]; !ok {
						db.deps[p.TypeName()] = map[string]struct{}{}
					}
					db.deps[p.TypeName()][n.TypeName()] = struct{}{}
				}
				break
			}
		}
	}
	n.scanned = true

	for _, p := range parents {
		findParent(p, db)
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

func EnableHook(v bool) {
	hookEnabled = v
}

func dumpRefs(addr2 uintptr, outfile string) {
	db := &AllocDb{all: map[uintptr]*Node{}, deps: map[string]map[string]struct{}{}}
	for _, r := range allocRecords {
		db.all[r.Base] = &Node{addr: r.Base, typ: r.Type(), arr: r.IsArr}
	}
	n := db.all[addr2]
	findParent(n, db)
	go func() {
		file, _ := os.OpenFile(outfile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		defer file.Close()
		file.WriteString("digraph {\n")
		dumpNodeDeps(db, file)
		file.WriteString("}\n")
	}()
}

func HexToUintptr(addr string) uintptr {
	addr = strings.Replace(addr, "0x", "", 1)
	addr1, err := strconv.ParseInt(addr, 16, 64)
	if err != nil {
		return 0
	}

	return uintptr(addr1)
}

func DumpRefsToSvg(addr uintptr, outfile string) error {
	EnableHook(false)
	defer EnableHook(true)

	runtime.GC()

	tmp := "tmp.dot"
	dumpRefs(addr, tmp)
	defer os.Remove(tmp)

	c := exec.Command("dot", "-Tsvg", tmp, "-o", outfile)
	return c.Run()
}
