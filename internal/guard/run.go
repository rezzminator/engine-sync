package guard

import (
	"errors"
	"sort"
	"time"
)

// Run locks canonical roots in sorted order and records even partial writes.
// Checks consult writer records without acquiring write locks.
func WithWriterGuard(state string, roots []string, version string, writer func() string, writes bool, wait time.Duration, body func() int) (int, error) {
	keys := make([]string, len(roots))
	for i, root := range roots {
		key, e := CanonicalRoot(root)
		if e != nil {
			return 1, e
		}
		keys[i] = key
	}
	sort.Strings(keys)
	unique := keys[:0]
	for _, key := range keys {
		if len(unique) == 0 || unique[len(unique)-1] != key {
			unique = append(unique, key)
		}
	}
	keys = unique
	if !writes {
		for _, key := range keys {
			if err := Check(state, key, version); err != nil {
				return 1, err
			}
		}
		return body(), nil
	}
	var releases []func() error
	releaseAll := func() error {
		var errs []error
		for i := len(releases) - 1; i >= 0; i-- {
			errs = append(errs, releases[i]())
		}
		return errors.Join(errs...)
	}
	for _, key := range keys {
		release, err := Lock(state, key, wait)
		if err != nil {
			return 1, errors.Join(err, releaseAll())
		}
		releases = append(releases, release)
	}
	for _, key := range keys {
		if err := Check(state, key, version); err != nil {
			return 1, errors.Join(err, releaseAll())
		}
	}
	code := body()
	var saveErr error
	{
		for _, key := range keys {
			saveErr = errors.Join(saveErr, Save(state, key, version, writer()))
		}
	}
	if err := releaseAll(); err != nil {
		saveErr = errors.Join(saveErr, err)
	}
	if saveErr != nil {
		return 1, saveErr
	}
	return code, nil
}
