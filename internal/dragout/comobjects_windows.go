package dragout

// Hand-built COM servers for OLE drag-and-drop. Windows has no equivalent
// of Fyne's simple event-monitor trick (see dragout_darwin.go) — starting a
// native drag means calling DoDragDrop with real IDropSource/IDataObject/
// IEnumFORMATETC implementations, which in Go means building each
// interface's vtable by hand: a struct of function pointers (created once
// via windows.NewCallback) whose layout mirrors the C++ vtable layout OLE
// expects, plus a Go object whose FIRST field is a pointer to that vtable —
// that's what makes &ourObject{} usable as a raw COM interface pointer.
//
// Every object we hand out to Windows this way is only visible to the Go
// runtime as a bare uintptr once cast for a syscall, which the garbage
// collector does not trace — see keepAliveSet below, which holds a real Go
// reference for as long as an object's refcount is nonzero, independent of
// whichever function's stack frame originally created it.
//
// go vet flags most of this file's unsafe.Pointer conversions as possible
// misuse — that check exists for uintptrs aliasing Go's GC-managed heap,
// where the referent could move between conversions. None of these do:
// every "this"-style uintptr here is either a raw OS-owned COM/OLE address
// (like GlobalLock's return in osclipboard_windows.go) or this package's
// own heap objects kept alive (and non-moving, per Go's current
// non-moving GC) via keepAliveSet for as long as any such conversion could
// observe them. Expected throughout this file, not addressed call-by-call.

import (
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Well-known, decades-stable OLE interface IDs (see objidl.h/unknwn.h).
var (
	iidIUnknown       = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDropSource    = windows.GUID{Data1: 0x00000121, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDataObject    = windows.GUID{Data1: 0x0000010e, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIEnumFormatEtc = windows.GUID{Data1: 0x00000103, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

const (
	sOK             = 0
	sFalse          = 1
	eNoInterface    = 0x80004002
	eNotImpl        = 0x80004001
	eFail           = 0x80004005
	dvENoFormatEtc  = 0x80040064
	dragDropSDrop   = 0x00040100
	dragDropSCancel = 0x00040101
	dragDropSUseDef = 0x00040102

	cfHDROP         = 15
	dvAspectContent = 1
	tymedHGlobal    = 1
	dataDirGet      = 1
	mkLButtonFlag   = 0x0001
)

// keepAliveSet holds a real Go pointer for every COM object we've handed to
// Windows and whose refcount is still nonzero — see the package doc above.
var (
	keepAliveMu  sync.Mutex
	keepAliveSet = map[unsafe.Pointer]bool{}
)

func keepAliveAdd(p unsafe.Pointer) { keepAliveMu.Lock(); keepAliveSet[p] = true; keepAliveMu.Unlock() }
func keepAliveRemove(p unsafe.Pointer) {
	keepAliveMu.Lock()
	delete(keepAliveSet, p)
	keepAliveMu.Unlock()
}

// ── FORMATETC / STGMEDIUM (objidl.h) ────────────────────────────────────────
// Field order matches the real C structs exactly; Go's automatic struct
// alignment (same rules as C for this purpose) reproduces the same byte
// layout OLE expects without any manual padding fields.

type formatEtc struct {
	cfFormat uint16
	ptd      uintptr
	dwAspect uint32
	lindex   int32
	tymed    uint32
}

type stgMedium struct {
	tymed          uint32
	union          uintptr // we only ever use this as an HGLOBAL
	pUnkForRelease uintptr
}

// dropFilesHeader mirrors the Win32 DROPFILES header — see the identical
// struct in internal/osclipboard/osclipboard_windows.go. Duplicated rather
// than shared: each internal package is a self-contained OS bridge, and
// this is the only piece they'd otherwise need to share.
type dropFilesHeader struct {
	pFiles uint32
	pt     struct{ x, y int32 }
	fNC    int32
	fWide  int32
}

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
)

const gmemMoveable = 0x0002

// newHDropGlobal builds a fresh CF_HDROP HGLOBAL from paths. A fresh one is
// built on every call (not cached/reused) because returning a medium with
// pUnkForRelease NULL transfers ownership to whoever called GetData — if
// more than one drop target ends up calling GetData during a single drag
// (e.g. a target that queries before actually accepting the drop), each
// must get its own independently-freeable copy, or the first free would
// leave the second with a dangling handle.
func newHDropGlobal(paths []string) (uintptr, error) {
	var buf []uint16
	for _, p := range paths {
		u, err := windows.UTF16FromString(p)
		if err != nil {
			return 0, err
		}
		buf = append(buf, u[:len(u)-1]...)
		buf = append(buf, 0)
	}
	buf = append(buf, 0)

	headerSize := int(unsafe.Sizeof(dropFilesHeader{}))
	dataSize := uintptr(headerSize + len(buf)*2)

	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, dataSize)
	if hMem == 0 {
		return 0, errors.New("GlobalAlloc failed")
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return 0, errors.New("GlobalLock failed")
	}
	// See osclipboard_windows.go's identical comment: ptr is a raw OS
	// memory address outside Go's GC-managed heap, so converting it
	// between uintptr and unsafe.Pointer here (including after pointer
	// arithmetic) is safe despite go vet's unsafeptr warning.
	header := (*dropFilesHeader)(unsafe.Pointer(ptr))
	*header = dropFilesHeader{pFiles: uint32(headerSize), fWide: 1}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr+uintptr(headerSize))), len(buf))
	copy(dst, buf)
	procGlobalUnlock.Call(hMem)
	return hMem, nil
}

// ── IDropSource ──────────────────────────────────────────────────────────

type iUnknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type iDropSourceVtbl struct {
	iUnknownVtbl
	QueryContinueDrag uintptr
	GiveFeedback      uintptr
}

// dropSourceObj's vtbl field MUST be first: that's what makes &dropSourceObj{}
// usable as a raw IDropSource* the OS can call methods on.
type dropSourceObj struct {
	vtbl     *iDropSourceVtbl
	refCount int32
}

// Built in init(), not as a var initializer expression: newDropSource
// (used nowhere near init) reads this var by name, and Go's static
// initialization-cycle detector treats that as a self-cycle even though
// it's never actually called during init — see the identical note on
// enumFormatEtcVtblInstance below, where this same shape is a hard error.
var dropSourceVtblInstance *iDropSourceVtbl

func newDropSource() *dropSourceObj {
	obj := &dropSourceObj{vtbl: dropSourceVtblInstance, refCount: 1}
	keepAliveAdd(unsafe.Pointer(obj))
	return obj
}

func dropSourceQueryInterface(this, riid, ppv uintptr) uintptr {
	obj := (*dropSourceObj)(unsafe.Pointer(this))
	iid := (*windows.GUID)(unsafe.Pointer(riid))
	if *iid == iidIUnknown || *iid == iidIDropSource {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		obj.refCount++
		return sOK
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	return eNoInterface
}

func dropSourceAddRef(this uintptr) uintptr {
	obj := (*dropSourceObj)(unsafe.Pointer(this))
	obj.refCount++
	return uintptr(obj.refCount)
}

func dropSourceRelease(this uintptr) uintptr {
	obj := (*dropSourceObj)(unsafe.Pointer(this))
	obj.refCount--
	if obj.refCount <= 0 {
		keepAliveRemove(unsafe.Pointer(obj))
	}
	return uintptr(obj.refCount)
}

// dropSourceQueryContinueDrag: cancel on Escape, complete the drop once the
// left button is no longer down, otherwise keep dragging.
func dropSourceQueryContinueDrag(this, fEscapePressed, grfKeyState uintptr) uintptr {
	if fEscapePressed != 0 {
		return dragDropSCancel
	}
	if grfKeyState&mkLButtonFlag == 0 {
		return dragDropSDrop
	}
	return sOK
}

// dropSourceGiveFeedback: let OLE show its own default cursors rather than
// us drawing custom drag cursors.
func dropSourceGiveFeedback(this, dwEffect uintptr) uintptr {
	return dragDropSUseDef
}

// ── IDataObject ──────────────────────────────────────────────────────────

type iDataObjectVtbl struct {
	iUnknownVtbl
	GetData               uintptr
	GetDataHere           uintptr
	QueryGetData          uintptr
	GetCanonicalFormatEtc uintptr
	SetData               uintptr
	EnumFormatEtc         uintptr
	DAdvise               uintptr
	DUnadvise             uintptr
	EnumDAdvise           uintptr
}

type dataObjectObj struct {
	vtbl     *iDataObjectVtbl // MUST be first field
	refCount int32
	paths    []string
}

// Built in init() — see dropSourceVtblInstance's comment above;
// dataObjectEnumFormatEtc references this var by name too.
var dataObjectVtblInstance *iDataObjectVtbl

func newDataObject(paths []string) *dataObjectObj {
	obj := &dataObjectObj{vtbl: dataObjectVtblInstance, refCount: 1, paths: paths}
	keepAliveAdd(unsafe.Pointer(obj))
	return obj
}

func dataObjectQueryInterface(this, riid, ppv uintptr) uintptr {
	obj := (*dataObjectObj)(unsafe.Pointer(this))
	iid := (*windows.GUID)(unsafe.Pointer(riid))
	if *iid == iidIUnknown || *iid == iidIDataObject {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		obj.refCount++
		return sOK
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	return eNoInterface
}

func dataObjectAddRef(this uintptr) uintptr {
	obj := (*dataObjectObj)(unsafe.Pointer(this))
	obj.refCount++
	return uintptr(obj.refCount)
}

func dataObjectRelease(this uintptr) uintptr {
	obj := (*dataObjectObj)(unsafe.Pointer(this))
	obj.refCount--
	if obj.refCount <= 0 {
		keepAliveRemove(unsafe.Pointer(obj))
	}
	return uintptr(obj.refCount)
}

// We only ever offer one format: CF_HDROP via an HGLOBAL. Everything else
// (bitmaps, IStream, etc.) is out of scope for "drag some files out."
func formatIsHDropHGlobal(fe *formatEtc) bool {
	return fe.cfFormat == cfHDROP && fe.tymed&tymedHGlobal != 0
}

func dataObjectGetData(this, pformatetcIn, pmedium uintptr) uintptr {
	obj := (*dataObjectObj)(unsafe.Pointer(this))
	fe := (*formatEtc)(unsafe.Pointer(pformatetcIn))
	if !formatIsHDropHGlobal(fe) {
		return dvENoFormatEtc
	}
	hMem, err := newHDropGlobal(obj.paths)
	if err != nil {
		return eFail
	}
	med := (*stgMedium)(unsafe.Pointer(pmedium))
	*med = stgMedium{tymed: tymedHGlobal, union: hMem}
	return sOK
}

func dataObjectQueryGetData(this, pformatetc uintptr) uintptr {
	fe := (*formatEtc)(unsafe.Pointer(pformatetc))
	if formatIsHDropHGlobal(fe) {
		return sOK
	}
	return dvENoFormatEtc
}

func dataObjectEnumFormatEtc(this, dwDirection, ppenumFormatEtc uintptr) uintptr {
	if dwDirection != dataDirGet {
		*(*uintptr)(unsafe.Pointer(ppenumFormatEtc)) = 0
		return eNotImpl
	}
	e := newEnumFormatEtc(false)
	*(*uintptr)(unsafe.Pointer(ppenumFormatEtc)) = uintptr(unsafe.Pointer(e))
	return sOK
}

// The rest of IDataObject is out of scope for a drag SOURCE that only ever
// offers one synchronous CF_HDROP: E_NOTIMPL is truthful (we don't support
// these operations) and is what a well-behaved OLE client expects here.
func dataObjectGetDataHere(this, pformatetc, pmedium uintptr) uintptr { return eNotImpl }
func dataObjectGetCanonicalFormatEtc(this, in, out uintptr) uintptr   { return eNotImpl }
func dataObjectSetData(this, pformatetc, pmedium, fRelease uintptr) uintptr {
	return eNotImpl
}
func dataObjectDAdvise(this, pformatetc, advf, pAdvSink, pdwConnection uintptr) uintptr {
	return eNotImpl
}
func dataObjectDUnadvise(this, dwConnection uintptr) uintptr { return eNotImpl }
func dataObjectEnumDAdvise(this, ppenumAdvise uintptr) uintptr {
	return eNotImpl
}

// ── IEnumFORMATETC ───────────────────────────────────────────────────────
// Minimal: we only ever enumerate the single CF_HDROP format dataObject
// offers, so "the cursor" is just a single bool (already returned or not).

type iEnumFormatEtcVtbl struct {
	iUnknownVtbl
	Next  uintptr
	Skip  uintptr
	Reset uintptr
	Clone uintptr
}

type enumFormatEtcObj struct {
	vtbl     *iEnumFormatEtcVtbl // MUST be first field
	refCount int32
	returned bool
}

// Built in init() — see dropSourceVtblInstance's comment above.
// enumFormatEtcClone (referenced below) calls newEnumFormatEtc, which
// reads this var by name: as a top-level initializer expression, that's a
// hard compile error ("initialization cycle"), since Go's static
// dependency analysis can't tell NewCallback merely takes the function's
// address here rather than calling it.
var enumFormatEtcVtblInstance *iEnumFormatEtcVtbl

func init() {
	dropSourceVtblInstance = &iDropSourceVtbl{
		iUnknownVtbl: iUnknownVtbl{
			QueryInterface: windows.NewCallback(dropSourceQueryInterface),
			AddRef:         windows.NewCallback(dropSourceAddRef),
			Release:        windows.NewCallback(dropSourceRelease),
		},
		QueryContinueDrag: windows.NewCallback(dropSourceQueryContinueDrag),
		GiveFeedback:      windows.NewCallback(dropSourceGiveFeedback),
	}

	dataObjectVtblInstance = &iDataObjectVtbl{
		iUnknownVtbl: iUnknownVtbl{
			QueryInterface: windows.NewCallback(dataObjectQueryInterface),
			AddRef:         windows.NewCallback(dataObjectAddRef),
			Release:        windows.NewCallback(dataObjectRelease),
		},
		GetData:               windows.NewCallback(dataObjectGetData),
		GetDataHere:           windows.NewCallback(dataObjectGetDataHere),
		QueryGetData:          windows.NewCallback(dataObjectQueryGetData),
		GetCanonicalFormatEtc: windows.NewCallback(dataObjectGetCanonicalFormatEtc),
		SetData:               windows.NewCallback(dataObjectSetData),
		EnumFormatEtc:         windows.NewCallback(dataObjectEnumFormatEtc),
		DAdvise:               windows.NewCallback(dataObjectDAdvise),
		DUnadvise:             windows.NewCallback(dataObjectDUnadvise),
		EnumDAdvise:           windows.NewCallback(dataObjectEnumDAdvise),
	}

	enumFormatEtcVtblInstance = &iEnumFormatEtcVtbl{
		iUnknownVtbl: iUnknownVtbl{
			QueryInterface: windows.NewCallback(enumFormatEtcQueryInterface),
			AddRef:         windows.NewCallback(enumFormatEtcAddRef),
			Release:        windows.NewCallback(enumFormatEtcRelease),
		},
		Next:  windows.NewCallback(enumFormatEtcNext),
		Skip:  windows.NewCallback(enumFormatEtcSkip),
		Reset: windows.NewCallback(enumFormatEtcReset),
		Clone: windows.NewCallback(enumFormatEtcClone),
	}
}

func newEnumFormatEtc(returned bool) *enumFormatEtcObj {
	obj := &enumFormatEtcObj{vtbl: enumFormatEtcVtblInstance, refCount: 1, returned: returned}
	keepAliveAdd(unsafe.Pointer(obj))
	return obj
}

func enumFormatEtcQueryInterface(this, riid, ppv uintptr) uintptr {
	obj := (*enumFormatEtcObj)(unsafe.Pointer(this))
	iid := (*windows.GUID)(unsafe.Pointer(riid))
	if *iid == iidIUnknown || *iid == iidIEnumFormatEtc {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		obj.refCount++
		return sOK
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	return eNoInterface
}

func enumFormatEtcAddRef(this uintptr) uintptr {
	obj := (*enumFormatEtcObj)(unsafe.Pointer(this))
	obj.refCount++
	return uintptr(obj.refCount)
}

func enumFormatEtcRelease(this uintptr) uintptr {
	obj := (*enumFormatEtcObj)(unsafe.Pointer(this))
	obj.refCount--
	if obj.refCount <= 0 {
		keepAliveRemove(unsafe.Pointer(obj))
	}
	return uintptr(obj.refCount)
}

func enumFormatEtcNext(this, celt, rgelt, pceltFetched uintptr) uintptr {
	obj := (*enumFormatEtcObj)(unsafe.Pointer(this))
	if obj.returned || celt == 0 {
		if pceltFetched != 0 {
			*(*uint32)(unsafe.Pointer(pceltFetched)) = 0
		}
		return sFalse
	}
	fe := (*formatEtc)(unsafe.Pointer(rgelt))
	*fe = formatEtc{cfFormat: cfHDROP, dwAspect: dvAspectContent, lindex: -1, tymed: tymedHGlobal}
	obj.returned = true
	if pceltFetched != 0 {
		*(*uint32)(unsafe.Pointer(pceltFetched)) = 1
	}
	if celt == 1 {
		return sOK
	}
	return sFalse // fewer elements returned than requested
}

func enumFormatEtcSkip(this, celt uintptr) uintptr {
	obj := (*enumFormatEtcObj)(unsafe.Pointer(this))
	if celt == 0 {
		return sOK
	}
	wasReturned := obj.returned
	obj.returned = true
	if !wasReturned && celt == 1 {
		return sOK
	}
	return sFalse
}

func enumFormatEtcReset(this uintptr) uintptr {
	obj := (*enumFormatEtcObj)(unsafe.Pointer(this))
	obj.returned = false
	return sOK
}

func enumFormatEtcClone(this, ppenum uintptr) uintptr {
	obj := (*enumFormatEtcObj)(unsafe.Pointer(this))
	clone := newEnumFormatEtc(obj.returned)
	*(*uintptr)(unsafe.Pointer(ppenum)) = uintptr(unsafe.Pointer(clone))
	return sOK
}
