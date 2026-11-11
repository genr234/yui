//go:build !windows

package native

// Place does nothing off Windows: there is no window to move. The rect maths
// (PlaceRect) is platform independent and tested.
func (SystemPlacer) Place(int, WindowSpec, <-chan struct{}, func(Rect, Screen)) {}
