package widget

import "RestoreSafe/internal/gui/win32"

// tipWidth is the width at which tooltips wrap, in DIPs.
const tipWidth = 420

// Tooltips are the tips of a group of controls, e.g. a card's: Clear
// removes them all before the controls are rebuilt.
type Tooltips struct {
	tip      win32.HWND
	controls []win32.HWND
}

// NewTooltips creates the tooltips of controls on windows owned by owner.
func NewTooltips(t *Theme, owner win32.HWND) (*Tooltips, error) {
	h, err := win32.NewTooltipWindow(owner, t.Scale.Px(tipWidth))
	if err != nil {
		return nil, err
	}
	return &Tooltips{tip: h}, nil
}

// Set shows text when the mouse rests on control; "" shows nothing.
func (t *Tooltips) Set(control win32.HWND, text string) {
	if t == nil || control == 0 || text == "" {
		return
	}
	win32.AddTooltip(t.tip, control, text)
	t.controls = append(t.controls, control)
}

// Clear removes the tips of all controls.
func (t *Tooltips) Clear() {
	if t == nil {
		return
	}
	for _, c := range t.controls {
		win32.RemoveTooltip(t.tip, c)
	}
	t.controls = nil
}
