package ref_dump

import "unsafe"

type NameOff int32
type TypeOff int32
type TextOff int32

type nameOff = NameOff
type typeOff = TypeOff
type textOff = TextOff

type ptabEntry struct {
	name nameOff
	typ  typeOff
}

type bitvector struct {
	n        int32 // # of bits
	bytedata *uint8
}

type modulehash struct {
	modulename   string
	linktimehash string
	runtimehash  *string
}

type functab struct {
	entryoff uint32 // relative to runtime.text
	funcoff  uint32
}

// Mapping information for secondary text sections

type textsect struct {
	vaddr    uintptr // prelinked section vaddr
	end      uintptr // vaddr + section length
	baseaddr uintptr // relocated section address
}

type Func struct {
	opaque struct{} // unexported field to disallow conversions
}

type initTask struct {
}

type itab struct {
}

// pcHeader holds data used by the pclntab lookups.
type pcHeader struct {
}

type moduledata struct {
	pcHeader     *pcHeader
	funcnametab  []byte
	cutab        []uint32
	filetab      []byte
	pctab        []byte
	pclntable    []byte
	ftab         []functab
	findfunctab  uintptr
	minpc, maxpc uintptr

	text, etext           uintptr
	noptrdata, enoptrdata uintptr
	data, edata           uintptr
	bss, ebss             uintptr
	noptrbss, enoptrbss   uintptr
	covctrs, ecovctrs     uintptr
	end, gcdata, gcbss    uintptr
	types, etypes         uintptr
	rodata                uintptr
	gofunc                uintptr // go.func.*

	textsectmap []textsect
	typelinks   []int32 // offsets from types
	itablinks   []*itab

	ptab []ptabEntry

	pluginpath string
	pkghashes  []modulehash

	// This slice records the initializing tasks that need to be
	// done to start up the program. It is built by the linker.
	inittasks []*initTask

	modulename   string
	modulehashes []modulehash

	hasmain uint8 // 1 if module contains the main function, 0 otherwise

	gcdatamask, gcbssmask bitvector

	typemap map[typeOff]*_type // offset to *_rtype in previous module

	bad bool // module failed to load and should be ignored

	next *moduledata
}

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

//go:linkname activeModules runtime.activeModules
func activeModules() []*moduledata
