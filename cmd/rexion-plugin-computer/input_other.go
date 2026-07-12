//go:build !windows

package main

import "fmt"

func mouseClick(x, y int, button string, double bool) error {
	return fmt.Errorf("input not supported on this platform")
}

func mouseScroll(x, y int, direction string, amount int) error {
	return fmt.Errorf("input not supported on this platform")
}

func mouseDrag(fromX, fromY, toX, toY int) error {
	return fmt.Errorf("input not supported on this platform")
}

func typeText(text string, clearFirst bool) error {
	return fmt.Errorf("input not supported on this platform")
}

func pressKey(key string, modifiers []string) error {
	return fmt.Errorf("input not supported on this platform")
}
