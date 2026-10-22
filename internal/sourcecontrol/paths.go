package sourcecontrol

import (
	"errors"
	"io"
	"os"
	"strings"

	"lara4ever/internal/workspacefiles"
)

// Retain each opened parent, and compare it with its name again after reading.
// os.Root bounds access; these checks additionally refuse links within the root
// and nested repositories, which must never inherit workspace authority.
type diskParent struct {
	root *os.Root
	name string
	info os.FileInfo
}
type diskPath struct {
	parents []diskParent
	roots   []*os.Root
	parent  *os.Root
	name    string
	file    *os.File
	info    os.FileInfo
	missing bool
}

func linkedPath(info os.FileInfo) bool {
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

func (p *diskPath) close() {
	if p.file != nil {
		p.file.Close()
	}
	for i := len(p.roots) - 1; i >= 0; i-- {
		p.roots[i].Close()
	}
}

func openDiskPath(root *os.Root, path string) (*diskPath, error) {
	if !workspacefiles.ValidRelativePath(path, false) {
		return nil, errors.New("Filename cannot be opened safely")
	}
	p := &diskPath{parent: root}
	success := false
	defer func() {
		if !success {
			p.close()
		}
	}()
	parts := strings.Split(path, "/")
	for i, name := range parts {
		if strings.EqualFold(name, ".git") {
			return nil, errors.New("Git administrative paths are unsupported")
		}
		info, err := p.parent.Lstat(name)
		if os.IsNotExist(err) {
			p.name, p.missing = name, true
			success = true
			return p, nil
		}
		if err != nil {
			return nil, err
		}
		if linkedPath(info) {
			return nil, errors.New("Linked paths are unsupported")
		}
		if i == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return nil, errors.New("Only regular files can be compared")
			}
			p.name, p.info = name, info
			p.file, err = p.parent.Open(name)
			if err != nil {
				return nil, err
			}
			opened, err := p.file.Stat()
			if err != nil {
				return nil, err
			}
			if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
				return nil, errors.New("File changed while opening")
			}
			p.info = opened
		} else {
			if !info.IsDir() {
				return nil, errors.New("Parent is not a directory")
			}
			child, err := p.parent.OpenRoot(name)
			if err != nil {
				return nil, err
			}
			p.roots = append(p.roots, child)
			opened, err := child.Stat(".")
			if err != nil {
				return nil, err
			}
			if !os.SameFile(info, opened) {
				return nil, errors.New("Parent changed while opening")
			}
			if _, err := child.Lstat(".git"); !os.IsNotExist(err) {
				if err != nil {
					return nil, err
				}
				return nil, errors.New("Nested repositories are unsupported")
			}
			p.parents = append(p.parents, diskParent{root: p.parent, name: name, info: opened})
			p.parent = child
		}
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	success = true
	return p, nil
}

func (p *diskPath) validate() error {
	for i, parent := range p.parents {
		info, err := parent.root.Lstat(parent.name)
		if err != nil {
			return err
		}
		if linkedPath(info) || !info.IsDir() || !os.SameFile(parent.info, info) {
			return errors.New("Parent changed during comparison")
		}
		if _, err := p.roots[i].Lstat(".git"); !os.IsNotExist(err) {
			if err != nil {
				return err
			}
			return errors.New("Nested repository appeared during comparison")
		}
	}
	info, err := p.parent.Lstat(p.name)
	if p.missing {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return errors.New("Path appeared during comparison")
	}
	if err != nil {
		return err
	}
	if linkedPath(info) || !info.Mode().IsRegular() || !os.SameFile(p.info, info) || info.Size() != p.info.Size() || !info.ModTime().Equal(p.info.ModTime()) {
		return errors.New("File changed during comparison")
	}
	return nil
}

func (p *diskPath) read() (DiffSide, string, error) {
	if p.missing {
		side, kind := decodeDiffText(nil)
		return side, kind, nil
	}
	if p.info.Size() > diffTextLimit {
		return DiffSide{Size: p.info.Size()}, "too_large", nil
	}
	data, err := io.ReadAll(io.LimitReader(p.file, diffTextLimit+1))
	if err != nil {
		return DiffSide{}, "", err
	}
	if err := p.validate(); err != nil {
		return DiffSide{}, "", err
	}
	if int64(len(data)) > diffTextLimit {
		return DiffSide{Size: int64(len(data))}, "too_large", nil
	}
	if int64(len(data)) != p.info.Size() {
		return DiffSide{}, "", errors.New("File size changed during comparison")
	}
	side, kind := decodeDiffText(data)
	return side, kind, nil
}
