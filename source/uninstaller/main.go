package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	user32      = syscall.NewLazyDLL("user32.dll")
	pMessageBox = user32.NewProc("MessageBoxW")
	advapi      = syscall.NewLazyDLL("advapi32.dll")
)

const regKey = "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\REC_Russian"

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func box(text string, flags uint32) int {
	r, _, _ := pMessageBox.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16("Удаление русификации REC"))), uintptr(flags|0x10000))
	return int(r)
}
func exists(p string) bool { _, e := os.Stat(p); return e == nil }

// restore moves the backup over the patched file.
func restore(orig, target string) error {
	if !exists(orig) {
		return nil
	}
	os.Remove(target)
	return os.Rename(orig, target)
}

func main() {
	exe, _ := os.Executable()
	game := filepath.Dir(exe)
	pck := filepath.Join(game, "REC.pck")
	dll := filepath.Join(game, "data_REC_windows_x86_64", "REC.dll")
	if !exists(pck+".original") && !exists(dll+".original") {
		box("Резервные копии оригинальных файлов не найдены.\nВозможно, русификация уже удалена.", 0x10)
		return
	}
	if box("Удалить русификацию и вернуть оригинальную игру?", 0x4|0x20) != 6 {
		return
	}
	if err := restore(pck+".original", pck); err != nil {
		box("Не удалось восстановить REC.pck. Закройте игру и повторите.\n"+err.Error(), 0x10)
		return
	}
	if err := restore(dll+".original", dll); err != nil {
		box("Не удалось восстановить REC.dll. Закройте игру и повторите.\n"+err.Error(), 0x10)
		return
	}
	// remove the "Apps & features" entry
	advapi.NewProc("RegDeleteKeyW").Call(0x80000001, uintptr(unsafe.Pointer(u16(regKey))))
	box("Русификация удалена. Игра возвращена к оригиналу.", 0x40)
	// self-delete after the process exits (retry until the file is unlocked)
	script := "for /l %i in (1,1,40) do (del /f /q \"" + exe + "\" 2>nul & if not exist \"" + exe + "\" exit /b & ping -n 2 127.0.0.1 >nul)"
	c := exec.Command("cmd")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000008, CmdLine: "cmd /c " + script}
	c.Start()
}
