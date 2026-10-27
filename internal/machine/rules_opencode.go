package machine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"github.com/rezzminator/engine-sync/internal/jsonc"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// compiledRules is the OpenCode instruction file's wanted bytes; "" when the
// file should not exist.
func compiledRules(set globalRules) string {
	if set.Body == "" {
		return ""
	}
	return artifact.GlobalMarker(artifact.Markdown, set.Label) + "\n\n" + set.Body
}

func (r *run) openCodeRules(set globalRules) error {
	compiled := compiledRulesFile(r.options.Home)
	want := compiledRules(set)
	if want == "" {
		// The entry goes before its file, so it never names a missing one:
		// an entry that could not be removed keeps the file it names.
		if err := r.openCodeEntry(compiled, false); err != nil {
			return err
		}
		return r.writeCompiledRules(compiled, want)
	}
	if err := r.writeCompiledRules(compiled, want); err != nil {
		return err
	}
	return r.openCodeEntry(compiled, true)
}

func (r *run) writeCompiledRules(path, want string) error {
	current, mode, err := readOwnedFile(path)
	if err != nil || current == want {
		return err
	}
	if want == "" {
		r.add(VerbChange, "delete "+path+" (global rules for OpenCode)", "")
		if r.options.Plan {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	}
	r.add(VerbChange, "write "+path+" (global rules for OpenCode)", "")
	if r.options.Plan {
		return nil
	}
	return atomicfile.Write(path, []byte(want), mode)
}

// instructionReceipt records the instructions entry engine-sync added, so it
// removes only its own: an entry the operator listed by hand is never claimed.
// Ownership follows Instruction, engine-sync's own compiled file; Config
// names where the entry was written and may since have moved behind a link.
type instructionReceipt struct {
	Config      string `json:"config"`
	Instruction string `json:"instruction"`
}

// instructionPlan is one comparison of the OpenCode config's instructions
// array with what engine-sync wants there.
type instructionPlan struct {
	Config        string
	Existed       bool
	Mode          fs.FileMode
	Raw           []byte
	Entries, Next []string
	Owned         instructionReceipt
	Claim         bool // engine-sync owns the wanted entry after the change
}

func planInstructions(home, instruction string, wanted bool) (instructionPlan, error) {
	plan := instructionPlan{Config: openCodeConfigPath(home)}
	receipt := instructionReceiptPath(home)
	raw, err := paths.ReadRegular(receipt)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return plan, fmt.Errorf("read OpenCode instruction receipt %s: %w", receipt, err)
	default:
		if err := json.Unmarshal(raw, &plan.Owned); err != nil {
			return plan, fmt.Errorf("parse OpenCode instruction receipt %s: %w", receipt, err)
		}
	}
	if physical, err := filepath.EvalSymlinks(plan.Config); err == nil {
		plan.Config = physical // edit a linked config where it lives
	}
	content, mode, err := readOwnedFile(plan.Config)
	if err != nil {
		return plan, err
	}
	plan.Existed, plan.Mode, plan.Raw = content != "", mode, []byte(content)
	if !plan.Existed {
		plan.Raw = []byte("{}\n")
		if _, err := os.Lstat(plan.Config); errors.Is(err, fs.ErrNotExist) {
			plan.Mode = 0o600 // a config engine-sync creates; an empty one keeps its mode
		} else if err != nil {
			return plan, fmt.Errorf("inspect OpenCode config %s: %w", plan.Config, err)
		}
	}
	document, err := jsonc.DecodeObject(plan.Raw)
	if err != nil {
		return plan, fmt.Errorf("parse OpenCode config %s: %w", plan.Config, err)
	}
	if plan.Entries, err = instructionEntries(document, plan.Config); err != nil {
		return plan, err
	}
	owns := plan.Owned.Instruction != ""
	plan.Next = []string{}
	for _, entry := range plan.Entries {
		if owns && entry == plan.Owned.Instruction && (entry != instruction || !wanted) {
			continue // engine-sync's own entry, moved or no longer wanted
		}
		plan.Next = append(plan.Next, entry)
	}
	if wanted {
		plan.Claim = owns && plan.Owned.Instruction == instruction
		if !slices.Contains(plan.Next, instruction) {
			plan.Next = append(plan.Next, instruction)
			plan.Claim = true
		}
	}
	return plan, nil
}

// instructionEntries reads the array; a present value of another shape is an
// error, never an empty list that would overwrite the operator's.
func instructionEntries(document map[string]any, path string) ([]string, error) {
	value, present := document[openCodeInstructionsKey]
	if !present || value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("OpenCode config %s: %s must be an array of file paths", path, openCodeInstructionsKey)
	}
	entries := make([]string, 0, len(items))
	for index, item := range items {
		entry, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("OpenCode config %s: %s[%d] must be a string", path, openCodeInstructionsKey, index)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (r *run) openCodeEntry(instruction string, wanted bool) error {
	plan, err := planInstructions(r.options.Home, instruction, wanted)
	if err != nil {
		return err
	}
	receipt := instructionReceiptPath(r.options.Home)
	if !slices.Equal(plan.Entries, plan.Next) && (plan.Existed || wanted) {
		var rendered []byte
		if len(plan.Next) == 0 {
			rendered, err = jsonc.RemoveProperty(plan.Raw, openCodeInstructionsKey)
		} else {
			var encoded []byte
			if encoded, err = encodeInstructions(plan.Next); err == nil {
				rendered, err = jsonc.SetProperty(plan.Raw, openCodeInstructionsKey, encoded)
			}
		}
		if err != nil {
			return fmt.Errorf("edit OpenCode config %s: %w", plan.Config, err)
		}
		r.add(VerbChange, "write "+plan.Config+" (instructions entry "+instruction+")", "")
		if r.options.Plan {
			return nil
		}
		// The receipt is written before the entry it claims, so a crash
		// between the two never leaves an entry engine-sync cannot remove.
		if plan.Claim {
			if err := writeInstructionReceipt(receipt, instructionReceipt{plan.Config, instruction}); err != nil {
				return err
			}
		}
		if err := atomicfile.Write(plan.Config, rendered, plan.Mode); err != nil {
			return err
		}
	}
	if r.options.Plan {
		return nil
	}
	if plan.Claim && plan.Owned != (instructionReceipt{plan.Config, instruction}) {
		return writeInstructionReceipt(receipt, instructionReceipt{plan.Config, instruction})
	}
	if !wanted && plan.Owned.Instruction != "" {
		if err := os.Remove(receipt); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove OpenCode instruction receipt %s: %w", receipt, err)
		}
	}
	return nil
}

// encodeInstructions renders the array compact, an operator's "&", "<" or ">"
// kept as written rather than escaped.
func encodeInstructions(entries []string) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(entries); err != nil {
		return nil, fmt.Errorf("encode OpenCode instructions: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func writeInstructionReceipt(path string, receipt instructionReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("encode OpenCode instruction receipt: %w", err)
	}
	if err := atomicfile.Write(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write OpenCode instruction receipt %s: %w", path, err)
	}
	return nil
}
