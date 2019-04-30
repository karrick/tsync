package main

import (
	"os"
	"path/filepath"
	"testing"
)

func parentAndBaseFromPathname(pathname string) (string, string, error) {
	if pathname != "." && pathname != ".." {
		return filepath.Dir(pathname), filepath.Base(pathname), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	if pathname == "." {
		return filepath.Dir(wd), filepath.Base(wd), nil
	}
	pathname = filepath.Dir(wd)
	return filepath.Dir(pathname), filepath.Base(pathname), nil
}

func TestParentAndBase(t *testing.T) {
	t.Run(".", func(t *testing.T) {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		parent, base, err := parentAndBaseFromPathname(".")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := parent, filepath.Dir(wd); got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
		if got, want := base, filepath.Base(wd); got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
	})
	t.Run("..", func(t *testing.T) {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		parent, base, err := parentAndBaseFromPathname("..")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := parent, filepath.Dir(filepath.Dir(wd)); got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
		if got, want := base, filepath.Base(filepath.Dir(wd)); got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
	})
	t.Run("./testdata", func(t *testing.T) {
		parent, base, err := parentAndBaseFromPathname("./testdata")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := parent, "."; got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
		if got, want := base, "testdata"; got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
	})
	t.Run("../tsync/testdata", func(t *testing.T) {
		parent, base, err := parentAndBaseFromPathname("../tsync/testdata")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := parent, "../tsync"; got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
		if got, want := base, "testdata"; got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
	})
	t.Run("testdata", func(t *testing.T) {
		parent, base, err := parentAndBaseFromPathname("testdata")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := parent, "."; got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
		if got, want := base, "testdata"; got != want {
			t.Errorf("GOT: %v; WANT: %v", got, want)
		}
	})
}
