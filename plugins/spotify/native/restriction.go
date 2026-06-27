package native

import (
	"strings"

	librespot "github.com/devgianlu/go-librespot"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
)

// mediaRestricted reports whether the given media is region-restricted for the
// supplied 2-letter country code. It is a faithful port of the unexported
// player.isMediaRestricted (which lives in the CGo-tainted player package).
//
// An empty/invalid country is treated as unrestricted: we don't reliably know
// the account country in the download-only path, and a wrong AES key request
// would fail later and trigger the YTM fallback anyway.
func mediaRestricted(media *librespot.Media, country string) bool {
	if len(country) != 2 {
		return false
	}

	contains := func(list string) bool {
		for i := 0; i+1 < len(list); i += 2 {
			if strings.EqualFold(list[i:i+2], country) {
				return true
			}
		}
		return false
	}

	for _, res := range media.Restriction() {
		switch ress := res.CountryRestriction.(type) {
		case *metadatapb.Restriction_CountriesAllowed:
			if len(ress.CountriesAllowed) == 0 {
				return true
			}
			return !contains(ress.CountriesAllowed)
		case *metadatapb.Restriction_CountriesForbidden:
			return contains(ress.CountriesForbidden)
		}
	}

	return false
}
