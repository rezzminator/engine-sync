package artifact

import "io/fs"

type Kind uint8

const (
	File Kind = iota
	Link
	Region
)

type Syntax uint8

const (
	Markdown Syntax = iota
	TOML
	JSONC
	YAMLFront
)

type Root struct {
	ValidateOnly   bool
	InspectForeign bool
	Suffix         string
	Boundary       string
	Path           string
	SourceRoots    []string
}
type SourceLink struct{ Path, Target, Prefix string }
type Artifact struct {
	NativeMode    string
	Merge         func([]byte) ([]byte, []string, []string)
	OptionalEmpty bool
	Kept          *SourceLink
	Path          string
	Kind          Kind
	Content       []byte
	Target        string
	Mode          fs.FileMode
	Source        string
	Syntax        Syntax
}
