//go:build windows

package art

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// DefaultCountry is the iTunes storefront used when no region is known.
const DefaultCountry = "US"

// CountryCode turns a region name into an ISO 3166 alpha-2 code. It takes a bare code ("jp"),
// a locale name ("ja-JP", "en_GB") or a Windows geo name; anything else, such as the numeric
// "001" for the world, gives "" so the caller falls back.
func CountryCode(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, "-_"); i >= 0 {
		s = s[i+1:]
	}
	if len(s) != 2 {
		return ""
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return ""
		}
	}
	return strings.ToUpper(s)
}

// SetCountry picks the iTunes storefront; an unusable value restores the default.
func (r *Resolver) SetCountry(c string) {
	c = CountryCode(c)
	if c == "" {
		c = DefaultCountry
	}
	r.mu.Lock()
	r.country = c
	r.mu.Unlock()
}

// Country is the storefront currently searched.
func (r *Resolver) Country() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.country
}

// normalizeName folds case and drops bracketed asides ("(feat. X)", "[Remastered]") and
// punctuation, so the same song spelled two ways compares equal.
func normalizeName(s string) string {
	var b strings.Builder
	depth := 0
	for _, c := range strings.ToLower(s) {
		switch {
		case c == '(' || c == '[' || c == '（':
			depth++
		case c == ')' || c == ']' || c == '）':
			if depth > 0 {
				depth--
			}
		case depth == 0 && (unicode.IsLetter(c) || unicode.IsDigit(c)):
			b.WriteRune(c)
		}
	}
	return b.String()
}

// sameTitle needs an exact match after normalising: a prefix would accept "Song" for "Song Part 2".
func sameTitle(a, b string) bool {
	a, b = normalizeName(a), normalizeName(b)
	return a != "" && a == b
}

// sameArtist also accepts one name inside the other, for "A & B" and "A feat. B" credits.
func sameArtist(a, b string) bool {
	a, b = normalizeName(a), normalizeName(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	return len([]rune(short)) >= 3 && strings.Contains(long, short)
}

// deezerArt is the fallback when iTunes has nothing. Unlike the iTunes search, which trusts its
// top hit, it takes a result only when both title and artist agree with the playing track.
func (r *Resolver) deezerArt(t Track) Lookup {
	if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Artist) == "" {
		return Lookup{}
	}
	key := keyOf("deezer", t.Title, t.Artist)
	r.mu.Lock()
	if e, ok := r.itu[key]; ok && time.Now().Before(e.exp) {
		r.mu.Unlock()
		return e.l
	}
	r.mu.Unlock()

	clean := func(s string) string { return strings.ReplaceAll(s, `"`, " ") }
	q := url.Values{}
	q.Set("q", `artist:"`+clean(t.Artist)+`" track:"`+clean(t.Title)+`"`)
	q.Set("limit", "5")
	req, err := http.NewRequest(http.MethodGet, r.deezerURL+"?"+q.Encode(), nil)
	if err != nil {
		return Lookup{}
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		log.Printf("art: deezer: %v", err)
		return Lookup{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("art: deezer: status %d", resp.StatusCode)
		return Lookup{}
	}
	var out struct {
		Data []struct {
			Title  string `json:"title"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
			Album struct {
				CoverXL  string `json:"cover_xl"`
				CoverBig string `json:"cover_big"`
			} `json:"album"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Lookup{}
	}
	l := Lookup{}
	for _, d := range out.Data {
		if !sameTitle(d.Title, t.Title) || !sameArtist(d.Artist.Name, t.Artist) {
			continue
		}
		img := d.Album.CoverXL
		if img == "" {
			img = d.Album.CoverBig
		}
		if u, err := url.Parse(img); err != nil || u.Scheme != "https" {
			continue
		}
		// No ListenURL: a Deezer page would sit under a button labelled for another service.
		l = Lookup{ImageURL: img, Via: "deezer"}
		break
	}
	r.remember(key, l)
	return l
}
