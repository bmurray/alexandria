package library

import (
	"reflect"
	"testing"
)

func TestWordsAndLikeTerms(t *testing.T) {
	if got := words("E2E Book 1791-530585 (x)"); !reflect.DeepEqual(got, []string{"E2E", "Book", "1791", "530585", "x"}) {
		t.Errorf("words = %q", got)
	}
	if got := likeTerms("Le-Guin earthsea"); !reflect.DeepEqual(got, []string{"%Le%", "%Guin%", "%earthsea%"}) {
		t.Errorf("likeTerms = %q", got)
	}
}
