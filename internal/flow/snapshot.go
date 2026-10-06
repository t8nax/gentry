package flow

import (
	"cmp"
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/t8nax/gentry/internal/process"
)

// Snapshot is the content of a flow version: the files of the draft it was
// applied from, the project subagents among them, and copies of the library
// subagents its stages name, so that editing the library does not change a
// version tasks are pinned to.
type Snapshot struct {
	Files   map[string]string `json:"files"`   // by path inside the flow, with forward slashes
	Library map[string]string `json:"library"` // by file name in the library, such as reviewer.yaml
}

// Encode returns the snapshot as stored in the state store.
func (s Snapshot) Encode() ([]byte, error) { return json.Marshal(s) }

// DecodeSnapshot reads a snapshot stored by Encode.
func DecodeSnapshot(b []byte) (Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return Snapshot{}, err
	}
	if s.Files == nil {
		s.Files = map[string]string{}
	}
	if s.Library == nil {
		s.Library = map[string]string{}
	}
	return s, nil
}

// Kinds of objects of a flow, as changes and the contract name them.
const (
	ObjectCommon   = "common"
	ObjectScenario = "scenario"
	ObjectStage    = "stage"
	ObjectPart     = "part"
	ObjectAgent    = "agent"
)

// Kinds of changes of an object.
const (
	Added    = "added"
	Modified = "modified"
	Removed  = "removed"
)

// Change is an object of a flow that differs between two snapshots.
type Change struct {
	Object  string // one of Object*
	ID      string // empty for ObjectCommon
	Library bool   // a subagent of the library, not of the project
	Change  string // Added, Modified or Removed
}

// objectKey identifies an object of a flow across snapshots.
type objectKey struct {
	object  string
	id      string
	library bool
}

// rankOf orders the kinds of objects as the flow lists them.
func rankOf(k objectKey) int {
	r := slices.Index([]string{ObjectCommon, ObjectScenario, ObjectStage, ObjectPart, ObjectAgent}, k.object)
	if k.library {
		r++
	}
	return r
}

// objects groups the files of s by the object they describe. A stage and a
// subagent have two files each; a file that belongs to no object is left out:
// such a flow has problems and cannot be applied.
func (s Snapshot) objects() map[objectKey]map[string]string {
	out := map[objectKey]map[string]string{}
	add := func(k objectKey, file, text string) {
		if out[k] == nil {
			out[k] = map[string]string{}
		}
		out[k][file] = text
	}
	for p, text := range s.Files {
		dir, name := path.Split(p)
		ext := path.Ext(name)
		id := strings.TrimSuffix(name, ext)
		switch {
		case p == commonFile:
			add(objectKey{object: ObjectCommon}, p, text)
		case dir == scenariosDir+"/" && ext == fieldsExt:
			add(objectKey{object: ObjectScenario, id: id}, p, text)
		case dir == stagesDir+"/" && (ext == fieldsExt || ext == textExt):
			add(objectKey{object: ObjectStage, id: id}, p, text)
		case dir == partsDir+"/" && ext == textExt:
			add(objectKey{object: ObjectPart, id: id}, p, text)
		case dir == agentsDir+"/" && (ext == fieldsExt || ext == textExt):
			add(objectKey{object: ObjectAgent, id: id}, p, text)
		}
	}
	for name, text := range s.Library {
		ext := path.Ext(name)
		if ext == fieldsExt || ext == textExt {
			add(objectKey{object: ObjectAgent, id: strings.TrimSuffix(name, ext), library: true}, name, text)
		}
	}
	return out
}

// Diff returns the objects that differ from old to cur: the common rules
// first, then scenarios, stages, parts, project subagents and library
// subagents, each kind by identifier. Texts are compared with line ends
// normalized.
func Diff(old, cur Snapshot) []Change {
	before, after := old.objects(), cur.objects()
	var changes []Change
	note := func(k objectKey, change string) {
		changes = append(changes, Change{Object: k.object, ID: k.id, Library: k.library, Change: change})
	}
	for k, files := range after {
		prev, ok := before[k]
		switch {
		case !ok:
			note(k, Added)
		case !sameFiles(prev, files):
			note(k, Modified)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			note(k, Removed)
		}
	}
	slices.SortFunc(changes, func(a, b Change) int {
		ka := objectKey{object: a.Object, id: a.ID, library: a.Library}
		kb := objectKey{object: b.Object, id: b.ID, library: b.Library}
		return cmp.Or(cmp.Compare(rankOf(ka), rankOf(kb)), strings.Compare(a.ID, b.ID))
	})
	return changes
}

func sameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, text := range a {
		other, ok := b[name]
		if !ok || string(process.Normalize([]byte(other))) != string(process.Normalize([]byte(text))) {
			return false
		}
	}
	return true
}
