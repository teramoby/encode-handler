package openid

import (
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// EncodingType is type for Encodings
type EncodingType string

const (
	// AES128GCM is the const for encoding aes128gcm
	AES128GCM EncodingType = "aes128gcm"
	// BR is the const for encoding br
	BR EncodingType = "br"
	// Compress is the const for encoding compress
	Compress EncodingType = "compress"
	// Deflate is the const for encoding deflate
	Deflate EncodingType = "deflate"
	// EXI is the const for encoding exi
	EXI EncodingType = "exi"
	// GZip is the const for encoding gzip
	GZip EncodingType = "gzip"
	// Identity is the const for encoding identity
	Identity EncodingType = "identity"
	// Pack200GZip is the const for encoding pack200-gzip
	Pack200GZip EncodingType = "pack200-gzip"
	// ZStd is the const for encoding zstd
	ZStd EncodingType = "zstd"
	// XCompress is the const for encoding x-compress
	XCompress EncodingType = "x-compress"
	// XGZip is the const for encoding x-gzip
	XGZip EncodingType = "x-gzip"
	// All is the const for encoding *
	All EncodingType = "*"
)

const preferEncoding = Identity

type acceptEncodingItem struct {
	encoding EncodingType
	qvalue   float64
}

type sortedAcceptEncodingList []acceptEncodingItem
type disabledEncodingMap map[EncodingType]bool

type acceptEncoding struct {
	sortAcceptEncodings sortedAcceptEncodingList
	disabledEncodings   disabledEncodingMap
}

// https://tools.ietf.org/html/rfc7231#section-5.3.1
const qvalueExp = "^q=((1(\\.0{0,3})?)|(0(\\.\\d{0,3})?))$"

type sortedAcceptEncodings []acceptEncoding

func verifyEncodingName(name string) EncodingType {
	enc := EncodingType(strings.TrimSpace(name))
	switch enc {
	case AES128GCM, BR, Compress, Deflate, EXI, GZip,
		Identity, Pack200GZip, ZStd, All:
		return enc
	case XCompress:
		return Compress
	case XGZip:
		return GZip
	default:
	}
	return ""
}

// For https://tools.ietf.org/html/rfc7231#section-5.3.1
func getQValue(qv string) float64 {
	qv = strings.TrimSpace(qv)
	if matched, err := regexp.MatchString(qvalueExp, qv); !matched || err != nil {
		if err != nil {
			log.Errorf("Error %v while match expression with %s.", err, qvalueExp)
		}
		return math.NaN()
	}

	num := qv[2:]
	// error can be ignored, because the input has already
	// verified by the regular expression
	ret, _ := strconv.ParseFloat(num, 64)
	return ret
}

func newAcceptEncoding() acceptEncoding {
	accEncoding := acceptEncoding{}
	accEncoding.disabledEncodings = make(disabledEncodingMap)
	accEncoding.sortAcceptEncodings = make(sortedAcceptEncodingList, 0)

	return accEncoding
}

func (a acceptEncoding) selectAcceptEncoding(encs map[EncodingType]bool, r *http.Request) EncodingType {
	a.parseRequest(r)
	for _, accenc := range a.sortAcceptEncodings {
		enc := accenc.encoding
		if accenc.encoding == All {
			// Return preferEncoding directly.
			// TODO, callers can set this in the future.
			enc = preferEncoding
		}

		if encs[enc] {
			// The encoding is suppoored by the handler
			if !a.disabledEncodings[enc] {
				return enc
			}

			// The coding is disabled
			continue
		}
	}

	return ""
}

func (a *acceptEncoding) parseRequest(r *http.Request) {
	values, ok := r.Header["Accept-Encoding"]
	if !ok {
		// No Accept-Encoding header found
		a.sortAcceptEncodings = append(a.sortAcceptEncodings,
			acceptEncodingItem{All, 1.0})
		return
	}

	if len(values) > 1 {
		log.Warnf("Multiple Accept-Encoding header found in request, the values are %v. Only the first one %s will be used.", values, values[0])
	}

	headerValue := values[0]
	if len(headerValue) == 0 {
		// Accept-Encoding is not found, returns identity directly.
		a.sortAcceptEncodings = append(a.sortAcceptEncodings,
			acceptEncodingItem{Identity, 1.0})
		return
	}

	// https://tools.ietf.org/html/rfc7231#section-3.1.2.1
	// The value of encoding is case-insensitive
	// So convert the value to lower case
	headerValue = strings.ToLower(headerValue)
	for _, oneEnc := range strings.Split(headerValue, ",") {
		a.addOneAcceptEncoding(oneEnc)
	}
	// sort
	sort.Slice(a.sortAcceptEncodings, func(i, j int) bool {
		if math.Abs(a.sortAcceptEncodings[i].qvalue-a.sortAcceptEncodings[j].qvalue) < 0.0001 {
			// The two qvalud are the same
			if a.sortAcceptEncodings[i].encoding == "*" {
				return false
			}
			if a.sortAcceptEncodings[j].encoding == "*" {
				return true
			}
			// Dont swap the two encodings with same qvalue.
			return false
		}
		return a.sortAcceptEncodings[i].qvalue > a.sortAcceptEncodings[j].qvalue
	})
}

func (a *acceptEncoding) addOneAcceptEncoding(oneEnc string) {
	fs := strings.Split(oneEnc, ";")
	if len(fs) < 1 || len(fs) > 2 {
		// This is an invalid Accept-Encoding defination
		return
	}
	encName := verifyEncodingName(fs[0])
	if len(encName) == 0 {
		// the encoding name doesn't have any content, this is an invalid Accept-Encoding defination
		return
	}
	item := acceptEncodingItem{encName, 1.0}
	if len(fs) == 2 {
		item.qvalue = getQValue(fs[1])
		if math.IsNaN(item.qvalue) {
			// This is an invalid qvalue.
			return
		}
		if item.qvalue-0.0 < 0.0001 {
			// Equals to zero, that means the encoding is disabled.
			a.disabledEncodings[encName] = true
			return
		}
	}

	a.sortAcceptEncodings = append(a.sortAcceptEncodings, item)
}

type gzipWriter struct {
	httpw http.ResponseWriter
	gzipw io.Writer
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	return g.gzipw.Write(b)
}

func (g *gzipWriter) WriteHeader(statusCode int) {
	g.httpw.WriteHeader(statusCode)
}

func (g *gzipWriter) Header() http.Header {
	return g.httpw.Header()
}

func gzipWrapper(next http.Handler, w http.ResponseWriter, r *http.Request) {
	// gzip
	gzipw := gzip.NewWriter(w)
	defer gzipw.Close()
	gw := gzipWriter{
		httpw: w,
		gzipw: gzipw,
	}
	gw.Header().Add("Content-Encoding", "gzip")
	next.ServeHTTP(&gw, r)
}

// EncodingHandler handles http requests with "Accept-Encoding" header
func EncodingHandler(allowedEncodingList []EncodingType, next http.Handler) (http.Handler, error) {
	if allowedEncodingList == nil || len(allowedEncodingList) == 0 {
		log.Warnf("Inputed allowedEncodingList is null or empty.")
		return next, fmt.Errorf("no item in allowedEncodingList")
	}
	allowedEncMap := make(map[EncodingType]bool, len(allowedEncodingList))
	for _, encStr := range allowedEncodingList {
		if enc := verifyEncodingName(string(encStr)); enc != "" {
			allowedEncMap[enc] = true
		} else {
			log.Warnf("Unknow encoding %s.", encStr)
		}
	}
	// No allowed encoding list was passed
	if len(allowedEncMap) == 0 {
		log.Warnf("No valid encoding in allowedEncodingList %v.", allowedEncodingList)
		return next, fmt.Errorf("no valid encoding in allowedEncodingList")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accencs := newAcceptEncoding()
		selenc := accencs.selectAcceptEncoding(allowedEncMap, r)

		switch selenc {
		case GZip:
			gzipWrapper(next, w, r)
			return
		case Identity:
			next.ServeHTTP(w, r)
			return
		}
		w.WriteHeader(http.StatusNotAcceptable)
	}), nil
}
