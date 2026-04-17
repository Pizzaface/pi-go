package piapi

import (
	"errors"
	"testing"
)

func TestSpec5ErrorsMatchSentinels(t *testing.T) {
	cases := []struct {
		err  error
		want error
	}{
		{ErrInvalidKind{Kind: "bad"}, ErrInvalidKindSentinel},
		{ErrIncoherentOptions{Reason: "x"}, ErrIncoherentOptionsSentinel},
		{ErrEntryNotFound{ID: "x"}, ErrEntryNotFoundSentinel},
		{ErrBranchNotFound{ID: "x"}, ErrBranchNotFoundSentinel},
		{ErrSessionNotFound{ID: "x"}, ErrSessionNotFoundSentinel},
		{ErrSessionControlUnsupportedInCLI{Method: "Fork"}, ErrSessionControlUnsupportedInCLISentinel},
		{ErrSessionControlInEventHandler{Method: "Fork"}, ErrSessionControlInEventHandlerSentinel},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.want) {
			t.Errorf("errors.Is(%T, %v) = false; want true", c.err, c.want)
		}
	}
}
