package model

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

type plan struct {
	name string
	sID  byte
	sURL string
	tURL string
	idx  uint64
	host string
	res  string
	err  error
}

func TestMakeShortURL(t *testing.T) {
	testPlan := []plan{
		{name: "sID=0, Idx=0", sID: 0, idx: 0, tURL: "http://localhost", res: "http://localhost/AAA"},
		{name: "sID=0, Idx=0", sID: 0, idx: 0, tURL: "http://localhost:8080", res: "http://localhost:8080/AAA"},
		{name: "sID=10, Idx=90", sID: 10, idx: 90, tURL: "http://localhost", res: "http://localhost/KWg"},
		{name: "sID=100, Idx=90", sID: 100, idx: 90, tURL: "http://localhost", err: ErrShardIDOverflow},
	}

	for _, p := range testPlan {
		t.Run(p.name, func(t *testing.T) {
			tURL, _ := url.Parse(p.tURL)
			sURL, err := MakeShortURL(p.sID, p.idx, *tURL)

			if p.err != nil {
				assert.ErrorIs(t, err, p.err)
			} else {
				assert.NoErrorf(t, err, "Error MakeShortURL(%d, %d, %s): %v", p.sID, p.idx, p.host, err)
				assert.Equal(t, p.res, sURL, "Wrong res=%s != sURL=%s%", p.res, sURL)

				sID, idx, err := ParseShortURL(sURL)

				assert.NoErrorf(t, err, "Error ParseShortURL(%s): %v", sURL, err)
				assert.Equal(t, p.sID, sID, "Wrong sID=%d", sID)
				assert.Equal(t, p.idx, idx, "Wrong idx=%d", idx)
			}
		})
	}
}

func TestParseShortURL(t *testing.T) {
	testPlan := []plan{
		{name: "sID=0, Idx=0", sURL: "http://localhost/AAA", sID: 0, idx: 0},
		{name: "sID=0, Idx=0", sURL: "http://localhost:8080/AAA", sID: 0, idx: 0},
		{name: "sID=10, Idx=90", sURL: "http://localhost/KWg", sID: 10, idx: 90},
		{name: "ErrParseURL", sURL: "http://localhost/K%tAE", err: ErrParseURL},
		{name: "ErrBadShardID", sURL: "http://localhost/~KtAE", err: ErrBadShardID},
		{name: "ErrBadB64U", sURL: "http://localhost/Kt~AE", err: ErrBadB64U},
		{name: "ErrBadBinIdx", sURL: "http://localhost/KtAEtAEtAEtAEtAEtAEtAEE", err: ErrBadBinIdx},
	}

	for _, p := range testPlan {
		t.Run(p.name, func(t *testing.T) {
			sID, idx, err := ParseShortURL(p.sURL)
			if p.err != nil {
				assert.ErrorIs(t, err, p.err)
			} else {
				assert.NoErrorf(t, err, "Error ParseShortURL(%s): %v", p.sURL, err)
				assert.Equal(t, p.sID, sID, "Wrong sID=%d", sID)
				assert.Equal(t, p.idx, idx, "Wrong idx=%d", idx)
			}
		})
	}
}
