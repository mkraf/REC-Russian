package main

import (
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// files/ must contain: ru_patch.xd3, REC_ru.dll, xdelta3.exe, REC_Russian_Uninstall.exe
//go:embed files/*
var fs embed.FS

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	advapi32     = syscall.NewLazyDLL("advapi32.dll")
	pRegCreate   = advapi32.NewProc("RegCreateKeyExW")
	pRegSetValue = advapi32.NewProc("RegSetValueExW")
	pRegClose    = advapi32.NewProc("RegCloseKey")

	pRegisterClassEx = user32.NewProc("RegisterClassExW")
	pCreateWindowEx  = user32.NewProc("CreateWindowExW")
	pDefWindowProc   = user32.NewProc("DefWindowProcW")
	pGetMessage      = user32.NewProc("GetMessageW")
	pTranslate       = user32.NewProc("TranslateMessage")
	pDispatch        = user32.NewProc("DispatchMessageW")
	pPostQuit        = user32.NewProc("PostQuitMessage")
	pShowWindow      = user32.NewProc("ShowWindow")
	pSendMessage     = user32.NewProc("SendMessageW")
	pPostMessage     = user32.NewProc("PostMessageW")
	pSetWindowText   = user32.NewProc("SetWindowTextW")
	pGetWindowText   = user32.NewProc("GetWindowTextW")
	pEnableWindow    = user32.NewProc("EnableWindow")
	pMessageBox      = user32.NewProc("MessageBoxW")
	pLoadCursor      = user32.NewProc("LoadCursorW")
	pLoadIcon        = user32.NewProc("LoadIconW")
	pAdjustRect      = user32.NewProc("AdjustWindowRect")
	pGetSysMetrics   = user32.NewProc("GetSystemMetrics")
	pDestroyWindow   = user32.NewProc("DestroyWindow")
	pSetForeground   = user32.NewProc("SetForegroundWindow")
	pGetModule       = kernel32.NewProc("GetModuleHandleW")
	pGetStockObject  = gdi32.NewProc("GetStockObject")
	pCreateFont      = gdi32.NewProc("CreateFontW")
	pSetBkMode       = gdi32.NewProc("SetBkMode")
	pBrowse          = shell32.NewProc("SHBrowseForFolderW")
	pPathFromIDList  = shell32.NewProc("SHGetPathFromIDListW")
	pCoInit          = ole32.NewProc("CoInitializeEx")
	pCoFree          = ole32.NewProc("CoTaskMemFree")
	pInitCC          = comctl32.NewProc("InitCommonControlsEx")
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}
type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      [2]int32
}
type rect struct{ L, T, R, B int32 }
type browseInfo struct {
	Owner   uintptr
	Root    uintptr
	Display *uint16
	Title   *uint16
	Flags   uint32
	Fn      uintptr
	LParam  uintptr
	Image   int32
}

const (
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmCommand      = 0x0111
	wmCtlColorStat = 0x0138
	wmSetFont      = 0x0030
	wmApp          = 0x8000
	idBrowse       = 101
	idNext         = 102
	idCancel       = 103
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsBorder       = 0x00800000
	wsTabStop      = 0x00010000
	wsExClientEdge = 0x200
	esAutoHScroll  = 0x80
	ssIcon         = 0x3
	ssEtchedHorz   = 0x10
	bsDefPush      = 1
	pbmMarquee     = 0x40A
	pbsMarquee     = 0x08
	stmSetIcon     = 0x170
	mbOK           = 0
	mbIconErr      = 0x10
	mbIconInfo     = 0x40
	mbYesNo        = 4
	mbIconQuestion = 0x20
	idYes          = 6
)

var (
	hInst, hwnd, hFont, hTitleFont                                     uintptr
	hEdit, hStatus, hProg, hTitle, hSub, hBtnNext, hBtnCancel, hBrowse uintptr
	page0, page1, page2                                                []uintptr
	hDone                                                              uintptr
	page                                                               int
	installing                                                         bool
	resultMsg                                                          string
	resultOK                                                           bool
)

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func mkWin(ex uint32, class, text string, style uint32, x, y, w, h int, id uintptr) uintptr {
	r, _, _ := pCreateWindowEx.Call(uintptr(ex), uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), hwnd, id, hInst, 0)
	if r != 0 {
		f := hFont
		pSendMessage.Call(r, wmSetFont, f, 1)
	}
	return r
}
func setText(h uintptr, s string) { pSetWindowText.Call(h, uintptr(unsafe.Pointer(u16(s)))) }
func show(list []uintptr, on bool) {
	c := uintptr(0)
	if on {
		c = 5
	}
	for _, h := range list {
		pShowWindow.Call(h, c)
	}
}
func box(text, title string, flags uint32) int {
	r, _, _ := pMessageBox.Call(hwnd, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), uintptr(flags))
	return int(r)
}
func getText(h uintptr) string {
	buf := make([]uint16, 1024)
	pGetWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), 1024)
	return syscall.UTF16ToString(buf)
}
func exists(p string) bool { _, e := os.Stat(p); return e == nil }

func defaultGame() string {
	c := []string{
		"C:\\Program Files (x86)\\Steam\\steamapps\\common\\REC",
		"C:\\Program Files\\Steam\\steamapps\\common\\REC",
		"D:\\SteamLibrary\\steamapps\\common\\REC",
		"D:\\Steam\\steamapps\\common\\REC",
		"E:\\SteamLibrary\\steamapps\\common\\REC",
	}
	for _, p := range c {
		if exists(filepath.Join(p, "REC.pck")) {
			return p
		}
	}
	return ""
}

func setPage(n int) {
	page = n
	show(page0, n == 0)
	show(page1, n == 1)
	show(page2, n == 2)
	switch n {
	case 0:
		setText(hTitle, "Русификация игры REC")
		setText(hSub, "Мастер установки русского перевода")
		setText(hBtnNext, "Установить")
	case 1:
		setText(hTitle, "Установка")
		setText(hSub, "Пожалуйста, подождите...")
		pEnableWindow.Call(hBtnNext, 0)
		pEnableWindow.Call(hBtnCancel, 0)
	case 2:
		setText(hTitle, "Готово")
		setText(hSub, "Установка завершена")
		pEnableWindow.Call(hBtnNext, 1)
		setText(hBtnNext, "Готово")
		show([]uintptr{hBtnCancel}, false)
	}
}

func onNext() {
	switch page {
	case 0:
		dir := getText(hEdit)
		if !exists(filepath.Join(dir, "REC.pck")) {
			box("В выбранной папке игра не найдена.\nУкажите папку, в которой находится REC.pck.", "Русификация REC", mbOK|mbIconErr)
			return
		}
		setPage(1)
		installing = true
		go func() {
			err := install(dir)
			if err != nil {
				resultOK = false
				resultMsg = "Ошибка установки:\r\n" + err.Error() + "\r\n\r\nОригинальные файлы не изменены."
			} else {
				resultOK = true
				resultMsg = "Русификация успешно установлена!\r\n\r\nЗапускайте игру как обычно (REC.exe).\r\nУдалить перевод: запустите REC_Russian_Uninstall.exe из папки с игрой."
			}
			pPostMessage.Call(hwnd, wmApp+2, 0, 0)
		}()
	case 2:
		pPostMessage.Call(hwnd, wmClose, 0, 0)
	}
}

func progress(s string) { setText(hStatus, s) }

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func install(game string) error {
	tmp, err := os.MkdirTemp("", "recru")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	progress("Распаковка файлов...")
	ents, _ := fs.ReadDir("files")
	for _, e := range ents {
		b, _ := fs.ReadFile("files/" + e.Name())
		if err := os.WriteFile(filepath.Join(tmp, e.Name()), b, 0644); err != nil {
			return err
		}
	}
	pck := filepath.Join(game, "REC.pck")
	orig := pck + ".original"
	if !exists(orig) {
		progress("Создание резервной копии REC.pck...")
		if err := copyFile(pck, orig); err != nil {
			return fmt.Errorf("не удалось создать резервную копию: %v", err)
		}
	}
	progress("Применение перевода (около минуты)...")
	newp := pck + ".new"
	cmd := exec.Command(filepath.Join(tmp, "xdelta3.exe"), "-f", "-d", "-s", orig, filepath.Join(tmp, "ru_patch.xd3"), newp)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(newp)
		return fmt.Errorf("не удалось применить патч (возможно, другая версия игры).\r\n%s", string(out))
	}
	if err := os.Rename(newp, pck); err != nil {
		os.Remove(pck)
		if err2 := os.Rename(newp, pck); err2 != nil {
			return fmt.Errorf("не удалось заменить REC.pck (закройте игру): %v", err)
		}
	}
	progress("Установка кода игры (REC.dll)...")
	dlldir := filepath.Join(game, "data_REC_windows_x86_64")
	dll := filepath.Join(dlldir, "REC.dll")
	if exists(dll) {
		if !exists(dll + ".original") {
			if err := copyFile(dll, dll+".original"); err != nil {
				return err
			}
		}
		if err := copyFile(filepath.Join(tmp, "REC_ru.dll"), dll); err != nil {
			return fmt.Errorf("не удалось заменить REC.dll (закройте игру): %v", err)
		}
	}
	un := filepath.Join(game, "REC_Russian_Uninstall.exe")
	if err := copyFile(filepath.Join(tmp, "REC_Russian_Uninstall.exe"), un); err == nil {
		registerUninstall(game, un)
	}
	return nil
}

func regSet(h uintptr, name, val string) {
	v := syscall.StringToUTF16(val)
	pRegSetValue.Call(h, uintptr(unsafe.Pointer(u16(name))), 0, 1, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
}

func registerUninstall(game, un string) {
	var h uintptr
	r, _, _ := pRegCreate.Call(0x80000001, uintptr(unsafe.Pointer(u16("Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\REC_Russian"))), 0, 0, 0, 0x20006, 0, uintptr(unsafe.Pointer(&h)), 0)
	if r != 0 {
		return
	}
	regSet(h, "DisplayName", "REC — Русификация")
	regSet(h, "DisplayVersion", "0.5.2")
	regSet(h, "Publisher", "REC Russian Translation")
	regSet(h, "InstallLocation", game)
	regSet(h, "DisplayIcon", un)
	regSet(h, "UninstallString", "\""+un+"\"")
	pRegClose.Call(h)
}

func browse() {
	buf := make([]uint16, 520)
	bi := browseInfo{Owner: hwnd, Display: &buf[0], Title: u16("Выберите папку с игрой REC:"), Flags: 0x1 | 0x40}
	pidl, _, _ := pBrowse.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return
	}
	pPathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&buf[0])))
	pCoFree.Call(pidl)
	setText(hEdit, syscall.UTF16ToString(buf))
}

func wndProc(h, m, wp, lp uintptr) uintptr {
	switch m {
	case wmCommand:
		switch wp & 0xffff {
		case idBrowse:
			browse()
		case idNext:
			onNext()
		case idCancel:
			if box("Прервать установку?", "Русификация REC", mbYesNo|mbIconQuestion) == idYes {
				pDestroyWindow.Call(h)
			}
		}
		return 0
	case wmCtlColorStat:
		pSetBkMode.Call(wp, 1)
		b, _, _ := pGetStockObject.Call(0) // WHITE_BRUSH
		return b
	case wmApp + 2:
		installing = false
		setPage(2)
		setText(hDone, resultMsg)
		if !resultOK {
			setText(hTitle, "Ошибка")
			setText(hSub, "Установка не выполнена")
		}
		return 0
	case wmClose:
		if installing {
			return 0
		}
	case wmDestroy:
		pPostQuit.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(h, m, wp, lp)
	return r
}

func main() {
	runtime.LockOSThread()
	pCoInit.Call(0, 2)
	icc := [2]uint32{8, 0x20 | 0x4000}
	pInitCC.Call(uintptr(unsafe.Pointer(&icc)))
	hInst, _, _ = pGetModule.Call(0)
	hFont, _, _ = pCreateFont.Call(^uintptr(14), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	hTitleFont, _, _ = pCreateFont.Call(^uintptr(21), 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	icon, _, _ := pLoadIcon.Call(hInst, 1)
	cur, _, _ := pLoadCursor.Call(0, 32512)
	cls := u16("RecRuSetup")
	wc := wndClassEx{Instance: hInst, Icon: icon, IconSm: icon, Cursor: cur, Background: 6, ClassName: cls, WndProc: syscall.NewCallback(wndProc)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

	const cw, ch = 520, 330
	r := rect{0, 0, cw, ch}
	style := uintptr(0x00C00000 | 0x00080000 | 0x00020000) // caption, sysmenu, minimize
	pAdjustRect.Call(uintptr(unsafe.Pointer(&r)), style, 0)
	sw, _, _ := pGetSysMetrics.Call(0)
	sh, _, _ := pGetSysMetrics.Call(1)
	ww, wh := int(r.R-r.L), int(r.B-r.T)
	hwnd, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16("Русификация REC"))), style,
		uintptr((int(sw)-ww)/2), uintptr((int(sh)-wh)/2), uintptr(ww), uintptr(wh), 0, 0, hInst, 0)

	vis := uint32(wsChild | wsVisible)
	ic := mkWin(0, "STATIC", "", vis|ssIcon, 22, 14, 36, 36, 0)
	pSendMessage.Call(ic, stmSetIcon, icon, 0)
	hTitle = mkWin(0, "STATIC", "", vis, 68, 12, 430, 28, 0)
	pSendMessage.Call(hTitle, wmSetFont, hTitleFont, 1)
	hSub = mkWin(0, "STATIC", "", vis, 68, 40, 430, 20, 0)
	mkWin(0, "STATIC", "", vis|ssEtchedHorz, 0, 66, cw, 2, 0)
	mkWin(0, "STATIC", "", vis|ssEtchedHorz, 0, 270, cw, 2, 0)

	// page 0
	p0a := mkWin(0, "STATIC", "Выберите папку, в которую установлена игра.", vis, 24, 86, 470, 24, 0)
	p0b := mkWin(0, "STATIC", "Папка игры:", vis, 24, 130, 300, 20, 0)
	hEdit = mkWin(wsExClientEdge, "EDIT", defaultGame(), vis|wsTabStop|esAutoHScroll, 24, 154, 370, 26, 0)
	hBrowse = mkWin(0, "BUTTON", "Обзор...", vis|wsTabStop, 404, 153, 90, 28, idBrowse)
	p0c := mkWin(0, "STATIC", "Перед установкой закройте игру.", vis, 24, 200, 470, 24, 0)
	page0 = []uintptr{p0a, p0b, hEdit, hBrowse, p0c}
	// page 1
	hStatus = mkWin(0, "STATIC", "Подготовка...", vis, 24, 100, 470, 22, 0)
	hProg = mkWin(0, "msctls_progress32", "", vis|pbsMarquee, 24, 134, 470, 20, 0)
	pSendMessage.Call(hProg, pbmMarquee, 1, 30)
	page1 = []uintptr{hStatus, hProg}
	// page 2
	hDone = mkWin(0, "STATIC", "", vis, 24, 90, 470, 150, 0)
	page2 = []uintptr{hDone}

	hBtnNext = mkWin(0, "BUTTON", "Установить", vis|wsTabStop|bsDefPush, 310, 284, 90, 30, idNext)
	hBtnCancel = mkWin(0, "BUTTON", "Отмена", vis|wsTabStop, 410, 284, 90, 30, idCancel)
	setPage(0)
	pShowWindow.Call(hwnd, 5)
	pSetForeground.Call(hwnd)
	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslate.Call(uintptr(unsafe.Pointer(&m)))
		pDispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
}
