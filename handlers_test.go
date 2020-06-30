package handler

import (
	"compress/gzip"
	"io/ioutil"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetQValue(t *testing.T) {
	cases := map[string]float64{
		"":         math.NaN(),
		"1.000":    math.NaN(), // no q=
		"q=":       math.NaN(), // only has q=
		"q=fdsa":   math.NaN(), // not a number
		"q=1.123":  math.NaN(), // should only be 1.000
		"q=1.0000": math.NaN(), // should be 1.000, 1.0, 1.00
		"q=0.0000": math.NaN(), // should be 0.000
		"q=0.1234": math.NaN(), // should be 0.123
		"q=2":      math.NaN(), // should not greater than 1
		"q=00.123": math.NaN(), // should be 0.123
		"q=22.000": math.NaN(), // invalid, should not be greater than 1
		"q=1.000":  1.0,
		"q=1.00":   1.0,
		"q=1.":     1.0,
		"q=0.":     0,
		"q=0.000":  0,
		"q=0.123":  0.123,
		"q=0.999":  0.999,
	}

	for key, value := range cases {
		ret := getQValue(key)
		if math.IsNaN(value) {
			if !math.IsNaN(ret) {
				t.Fatalf("Expected qvalue %f, but returned %f for case %s.", value, ret, key)
			}
			continue
		}
		// is not NaN
		if !(math.Abs(value-ret) < 0.0001) {
			t.Fatalf("Expected qvalue %f, but returned %f for case %s.", value, ret, key)
		}
	}
}

func TestVerifyEncodingName(t *testing.T) {
	cases := map[string]string{
		"aes128gcm":    "aes128gcm",
		"br":           "br",
		"compress":     "compress",
		"deflate":      "deflate",
		"exi":          "exi",
		"gzip":         "gzip",
		"identity":     "identity",
		"pack200-gzip": "pack200-gzip",
		"zstd":         "zstd",
		"x-compress":   "compress",
		"x-gzip":       "gzip",
		"*":            "*",
		"fdsafdsa":     "",
	}
	for key, value := range cases {
		ret := verifyEncodingName(key)
		if ret != EncodingType(value) {
			t.Fatalf("Incorrect return. %s expected, but %s returned.", value, ret)
		}
	}
}

func TestAddOneAcceptEncoding(t *testing.T) {
	encs := newAcceptEncoding()
	encs.addOneAcceptEncoding("")
	if len(encs.sortAcceptEncodings) != 0 {
		t.Fatal("No item should be added for empty encoding.")
	}

	encStr := "gzip;q=0;a=1"
	encs.addOneAcceptEncoding(encStr)
	if len(encs.sortAcceptEncodings) != 0 {
		t.Fatalf("No item should be added for invalid encoding %q.", encStr)
	}

	encStr = "fdsa;q=1"
	encs.addOneAcceptEncoding(encStr)
	if len(encs.sortAcceptEncodings) != 0 {
		t.Fatalf("No item should be added for invalid encoding %q.", encStr)
	}

	encStr = "fdsa;  q=1234"
	encs.addOneAcceptEncoding(encStr)
	if len(encs.sortAcceptEncodings) != 0 {
		t.Fatalf("No item should be added for invalid encoding %q.", encStr)
	}

	encStr = "gzip;  q=1234"
	encs.addOneAcceptEncoding(encStr)
	if len(encs.sortAcceptEncodings) != 0 {
		t.Fatalf("No item should be added for invalid encoding %q.", encStr)
	}

	encStr = "compress;  q=0"
	encs.addOneAcceptEncoding(encStr)
	if len(encs.sortAcceptEncodings) != 0 {
		t.Fatalf("No item should be added for invalid encoding %q.", encStr)
	}
	if _, ok := encs.disabledEncodings["compress"]; !ok {
		t.Fatalf("Encoding compress should be disabled for %q.", encStr)
	}

	encStr = "identity;q=1.0"
	encs.addOneAcceptEncoding(encStr)
	if len(encs.sortAcceptEncodings) != 1 {
		t.Fatalf("Only one encoding should be found while Accept-Encoding is %q.", encStr)
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], "identity", 1.0)

	encs.addOneAcceptEncoding("gzip")
	if len(encs.sortAcceptEncodings) != 2 {
		t.Fatal("Two encodings should be found here.")
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[1], "gzip", 1.0)
}

func TestParseRequest(t *testing.T) {
	encs := newAcceptEncoding()
	r := httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	encs.parseRequest(r)
	// verify if identity is present
	if len(encs.sortAcceptEncodings) != 1 {
		t.Fatal("Only one encoding should be found here.")
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], All, 1.0)

	encs = newAcceptEncoding()
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", "")
	encs.parseRequest(r)
	// verify if identity is present
	if len(encs.sortAcceptEncodings) != 1 {
		t.Fatal("Only one encoding should be found here.")
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], Identity, 1.0)

	encs = newAcceptEncoding()
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header["Accept-Encoding"] = []string{"", "gzip"}
	encs.parseRequest(r)
	// verify if identity is present
	if len(encs.sortAcceptEncodings) != 1 {
		t.Fatal("Only one encoding should be found here.")
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], Identity, 1.0)

	encs = newAcceptEncoding()
	encStr := "gzip;q=0.5"
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	encs.parseRequest(r)
	if len(encs.sortAcceptEncodings) != 1 {
		t.Fatalf("Only one encoding should be found while Accept-Encoding is %q.", encStr)
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], "gzip", 0.5)

	// verify two different encodings with same qvalue
	encs = newAcceptEncoding()
	encStr = "gzip,compress"
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	encs.parseRequest(r)
	if len(encs.sortAcceptEncodings) != 2 {
		t.Fatalf("Three encoding should be found while Accept-Encoding is %q.", encStr)
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], GZip, 1)
	verifyOneEncoding(t, encs.sortAcceptEncodings[1], Compress, 1)

	encs = newAcceptEncoding()
	encStr = "compress,gzip"
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	encs.parseRequest(r)
	if len(encs.sortAcceptEncodings) != 2 {
		t.Fatalf("Three encoding should be found while Accept-Encoding is %q.", encStr)
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], Compress, 1)
	verifyOneEncoding(t, encs.sortAcceptEncodings[1], GZip, 1)

	// All encoding has low priority
	encs = newAcceptEncoding()
	encStr = "compress,*"
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	encs.parseRequest(r)
	if len(encs.sortAcceptEncodings) != 2 {
		t.Fatalf("Three encoding should be found while Accept-Encoding is %q.", encStr)
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], Compress, 1)
	verifyOneEncoding(t, encs.sortAcceptEncodings[1], All, 1)

	// All encoding has low priority
	encs = newAcceptEncoding()
	encStr = "*,compress"
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	encs.parseRequest(r)
	if len(encs.sortAcceptEncodings) != 2 {
		t.Fatalf("Three encoding should be found while Accept-Encoding is %q.", encStr)
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], Compress, 1)
	verifyOneEncoding(t, encs.sortAcceptEncodings[1], All, 1)

	encs = newAcceptEncoding()
	encStr = "gzip;q=0.5,*;q=1,compress;q=0.8, identity;q=0"
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	encs.parseRequest(r)
	if len(encs.sortAcceptEncodings) != 3 {
		t.Fatalf("Three encoding should be found while Accept-Encoding is %q.", encStr)
	}
	verifyOneEncoding(t, encs.sortAcceptEncodings[0], All, 1)
	verifyOneEncoding(t, encs.sortAcceptEncodings[1], Compress, 0.8)
	verifyOneEncoding(t, encs.sortAcceptEncodings[2], GZip, 0.5)
	if _, ok := encs.disabledEncodings["identity"]; !ok {
		t.Fatalf("Encoding identity should be disabled for Accept-Encoding %q.", encStr)
	}
}

func TestSelectAcceptEncoding(t *testing.T) {
	supEncs := map[EncodingType]bool{
		GZip:     true,
		Identity: true,
	}

	encs := newAcceptEncoding()
	encStr := "gzip;q=0.5,*;q=1,compress;q=0.8, identity;q=0"
	r := httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	selected := encs.selectAcceptEncoding(supEncs, r)
	if selected != GZip {
		t.Fatalf("%s should be selected for encoding %s, but returned %s.", GZip, encStr, selected)
	}

	encs = newAcceptEncoding()
	encStr = "gzip;q=0.5,*;q=1,compress;q=0.8"
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	selected = encs.selectAcceptEncoding(supEncs, r)
	if selected != preferEncoding {
		t.Fatalf("%s should be selected for encoding %s, but returned %s.", preferEncoding, encStr, selected)
	}

	encs = newAcceptEncoding()
	encStr = "gzip;q=0.5,*;q=1,compress;q=0.8"
	r = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", encStr)
	selected = encs.selectAcceptEncoding(map[EncodingType]bool{}, r)
	if selected != "" {
		t.Fatalf("No Encoding should be selected, because the handler doesn't support any encodings.")
	}
}

var origh = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hello, world."))
})

func TestEncodingHandler(t *testing.T) {
	_, err := EncodingHandler(nil, origh)
	if err == nil {
		t.Fatalf("An error should be returned with nil encoding list.")
	}
	if err.Error() != "no item in allowedEncodingList" {
		t.Fatalf("The error message should be [no item in allowedEncodingList], but returned [%s].", err.Error())
	}

	_, err = EncodingHandler(nil, origh)
	if err == nil {
		t.Fatalf("An error should be returned with empty encoding list.")
	}
	if err.Error() != "no item in allowedEncodingList" {
		t.Fatalf("The error message should be [no item in allowedEncodingList], but returned [%s].", err.Error())
	}

	_, err = EncodingHandler([]EncodingType{"fdsafdsa"}, origh)
	if err == nil {
		t.Fatalf("An error should be returned while no valid encoding passed.")
	}
	if err.Error() != "no valid encoding in allowedEncodingList" {
		t.Fatalf("The error message should be [no valid encoding in allowedEncodingList], but returned [%s].", err.Error())
	}

	if _, err := EncodingHandler([]EncodingType{"fdsfdsa", GZip}, origh); err != nil {
		t.Fatalf("No error should be returned for a valid encoding.")
	}

	// Test if the encoding is not supported
	h, err := EncodingHandler([]EncodingType{GZip, EXI}, origh)
	if err != nil {
		t.Fatalf("No error should be returned for a valid encoding.")
	}

	r := httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", "EXI")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusNotAcceptable {
		t.Fatalf("Status %d should be returned while the inputted encoding is not supported, but returned %d.",
			http.StatusNotAcceptable, w.Result().StatusCode)
	}
}

func TestGZip(t *testing.T) {
	h, err := EncodingHandler([]EncodingType{GZip, EXI}, origh)
	if err != nil {
		t.Fatalf("No error should be returned for a valid encoding.")
	}
	r := httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	r.Header.Add("Accept-Encoding", string(GZip))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("Status %d should be returned for gzip but returned %d.",
			http.StatusOK, w.Result().StatusCode)
	}
	if w.Header().Get("Content-Encoding") != string(GZip) {
		t.Fatalf("Content-Encoding should be %s but %s was returned.",
			GZip, w.Header().Get("Content-Encoding"))
	}

	gr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("Unable to construct a new gzip reader due to error %v.", err)
	}
	buf, err := ioutil.ReadAll(gr)
	if err != nil {
		t.Fatalf("Unable to read body from reader due to error %v.", err)
	}
	if string(buf) != "Hello, world." {
		t.Fatalf("The body should be [%s], but returned [%s].", "Hello, world.", string(buf))
	}
}

func TestIdentity(t *testing.T) {
	h, err := EncodingHandler([]EncodingType{GZip, Identity}, origh)
	if err != nil {
		t.Fatalf("No error should be returned for a valid encoding.")
	}
	r := httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("Status %d should be returned for gzip but returned %d.",
			http.StatusOK, w.Result().StatusCode)
	}
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding should be %s but %s was returned.",
			Identity, w.Header().Get("Content-Encoding"))
	}

	buf, err := ioutil.ReadAll(w.Body)
	if err != nil {
		t.Fatalf("Unable to read body from reader due to error %v.", err)
	}
	if string(buf) != "Hello, world." {
		t.Fatalf("The body should be [%s], but returned [%s].", "Hello, world.", string(buf))
	}
}

func verifyOneEncoding(t *testing.T, item acceptEncodingItem, enc EncodingType, qvalue float64) {
	if item.encoding != enc || item.qvalue-qvalue > 0.0001 {
		t.Fatalf("Wrong encoding %v.", item)
	}
}
