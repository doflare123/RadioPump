package main

import "testing"

// Второй владелец не может запустить recovery, пока первый работает с библиотекой.
func TestLibraryLockRejectsSecondOwnerAndReleases(t *testing.T) {
	dir := t.TempDir()
	first, err := acquireLibraryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireLibraryLock(dir)
	if err == nil {
		_ = second.Close()
		_ = first.Close()
		t.Fatal("second owner accepted")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := acquireLibraryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}
