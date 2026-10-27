package bcc

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"mime"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	maxResponseBytes = 256 << 10
	maxFields        = 128
	maxFieldName     = 64
	maxFieldValue    = 1024
)

type PaymentStatus string

const StatusUnknown PaymentStatus = "unknown"

// Response contains only allowlisted gateway fields. ACTION/RC describe the
// received message; they are never interpreted as the original payment status.
// Raw bodies, P_SIGN, card data and bank tokens are deliberately not retained.
type Response struct {
	RequestType TransactionType
	HTTPStatus  int
	Action      string
	RC          string
	Order       string
	Terminal    string
	TRType      string
	// ReportedOriginalType preserves TRAN_TRTYPE as unverified metadata only.
	ReportedOriginalType string
	Amount               string
	Currency             string
	RRN                  string
	IntRef               string
}

// OriginalPaymentStatus deliberately remains unsupported until BCC provides
// the original-operation status schema. A status-query wrapper can be successful
// while the original transaction is pending, declined or absent.
func (Response) OriginalPaymentStatus() (PaymentStatus, error) {
	return StatusUnknown, ErrOriginalStatusUnknown
}

// String and GoString keep financial references out of accidental log formatting.
func (Response) String() string   { return "bcc gateway response (redacted)" }
func (Response) GoString() string { return "bcc.Response{redacted}" }

// ParseResponse supports string-valued JSON objects, URL-encoded fields and
// XML-compatible HTML input forms. JSON is an observed sandbox response format,
// not a guarantee of the original-operation status contract.
// HTML that the standard XML decoder cannot parse is explicitly unsupported;
// this package never executes scripts or guesses fields from malformed markup.
func ParseResponse(kind TransactionType, contentType string, body []byte) (Response, error) {
	out := Response{RequestType: kind}
	if len(body) > maxResponseBytes {
		return out, ErrResponseTooLarge
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		return out, ErrUnsupportedResponse
	}
	var fields url.Values
	switch mediaType {
	case "application/json":
		fields, err = jsonFields(body)
	case "application/x-www-form-urlencoded":
		fields, err = url.ParseQuery(string(body))
	case "text/html", "application/xhtml+xml":
		fields, err = htmlInputFields(body)
	default:
		return out, ErrUnsupportedResponse
	}
	if err != nil || len(fields) > maxFields {
		return out, ErrUnsupportedResponse
	}
	for name, values := range fields {
		if len(name) == 0 || len(name) > maxFieldName || !utf8.ValidString(name) || len(values) != 1 || len(values[0]) > maxFieldValue || !utf8.ValidString(values[0]) {
			return out, ErrUnsupportedResponse
		}
	}
	out.Action, out.RC = fields.Get("ACTION"), fields.Get("RC")
	if out.Action == "" || out.RC == "" || !shortGatewayCode(out.Action) || !shortGatewayCode(out.RC) {
		return Response{RequestType: kind}, ErrUnsupportedResponse
	}
	out.Order, out.Terminal, out.TRType = fields.Get("ORDER"), fields.Get("TERMINAL"), fields.Get("TRTYPE")
	out.ReportedOriginalType = fields.Get("TRAN_TRTYPE")
	out.Amount, out.Currency = fields.Get("AMOUNT"), fields.Get("CURRENCY")
	out.RRN, out.IntRef = fields.Get("RRN"), fields.Get("INT_REF")
	return out, nil
}

func jsonFields(body []byte) (url.Values, error) {
	if !utf8.Valid(body) {
		return nil, ErrUnsupportedResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, ErrUnsupportedResponse
	}
	fields := make(url.Values)
	for decoder.More() {
		key, err := decoder.Token()
		name, isString := key.(string)
		if err != nil || !isString || len(fields) >= maxFields || fields.Has(name) {
			return nil, ErrUnsupportedResponse
		}
		token, err := decoder.Token()
		value, isString := token.(string)
		if err != nil || !isString {
			return nil, ErrUnsupportedResponse
		}
		fields.Set(name, value)
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, ErrUnsupportedResponse
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrUnsupportedResponse
	}
	return fields, nil
}

func shortGatewayCode(value string) bool {
	return len(value) <= 16 && printableASCII(value) && !strings.ContainsAny(value, " \t\r\n")
}

func htmlInputFields(body []byte) (url.Values, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity
	fields := make(url.Values)
	inputs, tokens, depth := 0, 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return fields, nil
		}
		if err != nil {
			return nil, ErrUnsupportedResponse
		}
		tokens++
		if tokens > 10000 {
			return nil, ErrUnsupportedResponse
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if depth > 64 {
				return nil, ErrUnsupportedResponse
			}
			if strings.EqualFold(element.Name.Local, "script") || strings.EqualFold(element.Name.Local, "style") {
				if err := decoder.Skip(); err != nil {
					return nil, ErrUnsupportedResponse
				}
				depth--
				continue
			}
			if !strings.EqualFold(element.Name.Local, "input") {
				continue
			}
			inputs++
			if inputs > maxFields {
				return nil, ErrUnsupportedResponse
			}
			attrs := make(map[string]string)
			for _, attr := range element.Attr {
				name := strings.ToLower(attr.Name.Local)
				if _, exists := attrs[name]; exists || len(attr.Value) > maxFieldValue {
					return nil, ErrUnsupportedResponse
				}
				attrs[name] = attr.Value
			}
			if name := attrs["name"]; name != "" {
				fields.Add(name, attrs["value"])
			}
		case xml.EndElement:
			depth--
		}
	}
}
