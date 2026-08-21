package apperr

import (
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCodeOfPreservesWrappedApplicationError(t *testing.T) {
	cause := errors.New("unique constraint")
	err := Wrap(CodeConflict, "mission already exists", cause)
	wrapper := fmt.Errorf("service create: %w", err)

	require.Equal(t, CodeConflict, CodeOf(wrapper))
	require.ErrorIs(t, wrapper, cause)
	require.Contains(t, wrapper.Error(), "mission already exists")
}

func TestCodeOfUnknownDefaultsToInternal(t *testing.T) {
	require.Equal(t, CodeInternal, CodeOf(errors.New("plain error")))
	require.Equal(t, CodeInternal, CodeOf(nil))
}

func TestNewHasNoCause(t *testing.T) {
	err := New(CodeNotFound, "asset not found")
	var app *Error
	require.ErrorAs(t, err, &app)
	require.Equal(t, CodeNotFound, app.Code)
	require.Equal(t, "asset not found", app.Message)
	require.Nil(t, app.Unwrap())
}

func TestWrapExposesCause(t *testing.T) {
	cause := errors.New("database locked")
	err := Wrap(CodeUnavailable, "storage unavailable", cause)
	require.ErrorIs(t, err, cause)
	require.Equal(t, "storage unavailable: database locked", err.Error())
}

func TestCodesRemainDistinct(t *testing.T) {
	codes := []Code{CodeInvalid, CodeUnauthenticated, CodeForbidden, CodeNotFound, CodeConflict, CodeUnavailable, CodeInternal}
	seen := map[Code]bool{}
	for _, code := range codes {
		require.False(t, seen[code])
		seen[code] = true
	}
}
