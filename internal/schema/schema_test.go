package schema

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/colligo-go/scribo/internal/exitcode"
)

func TestReadFailureInsideTheTSVIsReturnedForTheCallerToClassify(t *testing.T) {
	failure := errors.New("input/output error")
	for name, reader := range map[string]io.Reader{
		"in the header":    iotest.ErrReader(failure),
		"after the header": io.MultiReader(strings.NewReader("sample\tvalue\nS1\t1\n"), iotest.ErrReader(failure)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := FromTSV("t", reader, []string{"sample"})

			var domain *exitcode.Error
			if !errors.Is(err, failure) || errors.As(err, &domain) {
				t.Errorf("error = %v, want the read failure itself", err)
			}
		})
	}
}
