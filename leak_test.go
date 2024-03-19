package ref_dump_test

import (
	"testing"
	"unsafe"

	"github.com/crazybie/ref_dump"
)

type Info struct {
	cb []func()
}

var G struct {
	info *Info
}

type MapBase struct {
	i int
}

func (m *MapBase) Foo() {

}

func TestDumpLeakedRefs(t *testing.T) {
	ref_dump.InitHooks()

	var leakedObj uintptr
	func() {
		m := &MapBase{}
		G.info = &Info{}
		G.info.cb = append(G.info.cb, m.Foo)
		leakedObj = (uintptr)(unsafe.Pointer(m))
	}()

	ref_dump.DumpRefs(leakedObj, "leaks.svg")
}
