package win32

import (
	"os"
	"runtime"
	"testing"
	"unsafe"
)

const emCanUndo = 0x00C6

func TestReadSecretReturnsUTF8AndClearsTheControl(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	edit, err := CreateWindow(0, "EDIT", "", ES_PASSWORD|ES_AUTOHSCROLL, 0, 0, 200, 24, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer DestroyWindow(edit)

	const secret = "pässwörd 😀 €"
	// EM_REPLACESEL, like typing, so there is something to undo.
	SendMessage(edit, EM_REPLACESEL, 1, uintptr(unsafe.Pointer(UTF16(secret))))
	if r := SendMessage(edit, emCanUndo, 0, 0); r == 0 {
		t.Fatal("test setup: the typed text should be undoable")
	}

	got := ReadSecret(edit)
	if string(got) != secret {
		t.Fatalf("ReadSecret = %q, want %q", got, secret)
	}
	if cap(got) < len(secret) || len(got) != len(secret) {
		t.Fatalf("unexpected length %d (cap %d)", len(got), cap(got))
	}
	if text := Text(edit); text != "" {
		t.Fatalf("the control must be empty afterwards, got %q", text)
	}
	if r := SendMessage(edit, emCanUndo, 0, 0); r != 0 {
		t.Fatal("undo must not bring the secret back")
	}
}

// TestReadSecretReadsAnUnmaskedFieldTheSameWay: the typed recovery code is
// a secret in a field without dots; it is read and cleared like a password
// (GUI spec 13.1, RF-119).
func TestReadSecretReadsAnUnmaskedFieldTheSameWay(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	edit, err := CreateWindow(0, "EDIT", "", ES_AUTOHSCROLL, 0, 0, 200, 24, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer DestroyWindow(edit)
	const code = "K7Q2-M9XD-4TPR-8WZN-C3HF-6JBV"
	SendMessage(edit, EM_REPLACESEL, 1, uintptr(unsafe.Pointer(UTF16(code))))

	if got := ReadSecret(edit); string(got) != code {
		t.Fatalf("ReadSecret = %q, want %q", got, code)
	}
	if text := Text(edit); text != "" {
		t.Fatalf("the control must be empty afterwards, got %q", text)
	}
	if r := SendMessage(edit, emCanUndo, 0, 0); r != 0 {
		t.Fatal("undo must not bring the code back")
	}
}

// TestClearSecretCopyLeavesWhatWasCopiedSince uses the real clipboard and
// empties it, so it runs only with RESTORESAFE_CLIPBOARD_TEST=1 (GUI spec
// 13.3, RF-119; the manual row CR-2 checks the same before a release).
func TestClearSecretCopyLeavesWhatWasCopiedSince(t *testing.T) {
	if os.Getenv("RESTORESAFE_CLIPBOARD_TEST") != "1" {
		t.Skip("uses the clipboard; set RESTORESAFE_CLIPBOARD_TEST=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := CreateWindow(0, "STATIC", "", 0, 0, 0, 10, 10, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer DestroyWindow(owner)

	seq, err := CopySecret(owner, []byte("SECRET-CODE"))
	if err != nil || seq == 0 {
		t.Fatalf("CopySecret: %d, %v", seq, err)
	}
	if !ClearSecretCopy(owner, seq) || ClipboardSequence() == seq {
		t.Fatal("the code must be taken off the clipboard")
	}

	seq, err = CopySecret(owner, []byte("SECRET-CODE"))
	if err != nil {
		t.Fatal(err)
	}
	if err := CopyText(owner, "something the user copied"); err != nil {
		t.Fatal(err)
	}
	after := ClipboardSequence()
	if ClearSecretCopy(owner, seq) || ClipboardSequence() != after {
		t.Fatal("something copied since must be left alone")
	}
}

func TestEncodeRuneMatchesUTF8(t *testing.T) {
	t.Parallel()
	for _, r := range []rune{'a', 'ä', '€', '😀', 0x7F, 0x80, 0x7FF, 0x800, 0xFFFF, 0x10000, 0x10FFFF} {
		var p [4]byte
		n := encodeRune(p[:], r)
		if want := string(r); string(p[:n]) != want {
			t.Errorf("encodeRune(%U) = % x, want % x", r, p[:n], []byte(want))
		}
	}
}

// TestListViewStructSizes keeps the list view structs at their x64 SDK
// sizes.
func TestListViewStructSizes(t *testing.T) {
	for name, got := range map[string]uintptr{
		"LVITEMW":        unsafe.Sizeof(lvItem{}),
		"LVGROUP":        unsafe.Sizeof(lvGroup{}),
		"LVCOLUMNW":      unsafe.Sizeof(lvColumn{}),
		"LVHITTESTINFO":  unsafe.Sizeof(lvHitTestInfo{}),
		"NMLVCUSTOMDRAW": unsafe.Sizeof(NMLVCustomDraw{}),
		"NMLISTVIEW":     unsafe.Sizeof(NMListView{}),
		"NMLVGETINFOTIP": unsafe.Sizeof(NMLVGetInfoTip{}),
	} {
		want := map[string]uintptr{"LVITEMW": 88, "LVGROUP": 152, "LVCOLUMNW": 56, "LVHITTESTINFO": 24, "NMLVCUSTOMDRAW": 136, "NMLISTVIEW": 64, "NMLVGETINFOTIP": 64}[name]
		if got != want {
			t.Errorf("%s: %d bytes, want %d", name, got, want)
		}
	}
}
