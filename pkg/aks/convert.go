package aks

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
)

// String returns a string value for the passed string pointer. It returns the empty string if the
// pointer is nil.
func String(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// Bool returns a bool value for the passed bool pointer. It returns false if the pointer is nil.
func Bool(b *bool) bool {
	if b != nil {
		return *b
	}
	return false
}

// StringSlice returns a string slice value for the passed string slice pointer. It returns a nil
// slice if the pointer is nil.
func StringSlice(s *[]string) []string {
	if s != nil {
		return *s
	}
	return nil
}

// StringMapPtr returns a map of string pointers built from the passed map of strings.
func StringMapPtr(ms map[string]string) map[string]*string {
	msp := make(map[string]*string, len(ms))
	for k, s := range ms {
		msp[k] = to.Ptr(s)
	}
	return msp
}

// StringMap returns a map of strings built from the map of string pointers. The empty string is
// used for nil pointers.
func StringMap(msp map[string]*string) map[string]string {
	ms := make(map[string]string, len(msp))
	for k, sp := range msp {
		if sp != nil {
			ms[k] = *sp
		} else {
			ms[k] = ""
		}
	}
	return ms
}

// Int32 returns an int value for the passed int pointer. It returns 0 if the pointer is nil.
func Int32(i *int32) int32 {
	if i != nil {
		return *i
	}
	return 0
}
