//go:build windows

package web

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

// On Windows the helper calls the Windows API directly instead of starting
// other programs: ShellExecuteW opens the browser, SHBrowseForFolderW shows
// the standard "choose a folder" window.

var (
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	ole32                   = syscall.NewLazyDLL("ole32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	procShellExecuteW       = shell32.NewProc("ShellExecuteW")
	procSHBrowseForFolderW  = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDList = shell32.NewProc("SHGetPathFromIDListW")
	procCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	procCoUninitialize      = ole32.NewProc("CoUninitialize")
	procCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
)

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	const swShowNormal = 1
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, swShowNormal)
	if r <= 32 {
		return errors.New("could not open the browser")
	}
	return nil
}

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	LParam      uintptr
	Image       int32
}

// BrowseFolder shows the Windows folder picker. It returns "" when the user cancels.
func BrowseFolder(title, start string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const coinitApartmentThreaded = 0x2
	procCoInitializeEx.Call(0, coinitApartmentThreaded)
	defer procCoUninitialize.Call()

	owner, _, _ := procGetForegroundWindow.Call() // keeps the window in front of the browser
	name := make([]uint16, 260)
	t, _ := syscall.UTF16PtrFromString(title)
	const bifReturnOnlyFSDirs, bifEditBox, bifNewDialogStyle = 0x1, 0x10, 0x40
	bi := browseInfo{Owner: owner, DisplayName: &name[0], Title: t, Flags: bifReturnOnlyFSDirs | bifEditBox | bifNewDialogStyle}
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", nil
	}
	defer procCoTaskMemFree.Call(pidl)
	path := make([]uint16, 32768)
	if ok, _, _ := procSHGetPathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); ok == 0 {
		return "", errors.New("this is not a folder on disk")
	}
	return syscall.UTF16ToString(path), nil
}
