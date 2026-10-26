// Package data provides private merchant-registration validation, normalization, and progress assessment.
// focodebase/fobackend/internal/data/merchant_registration_validation.go
//
// registration facts.
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Foundation Level 4 (Become a Merchant) requires eight registration facts
//	  whose validation must be identical across the Angular client, the
//	  service layer, persistence, and final completion. This file is the
//	  authoritative server-side rule set. The Angular mirror lives in
//	  fofrontend/src/app/merchant-onboarding/validation/
//	  merchant-registration-validation.ts and must change together with it.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Keep validation pure: no database access, no logging, no I/O.
//	Never include submitted values in errors. Field errors carry stable codes
//	only, because registration facts are private.
//	Keep draft validation (format only) separate from completion validation
//	(format plus required facts).
//	Database constraints enforce bounds and lifecycle integrity. The service
//	applies the full semantic rules on every write and completion.
package data

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrMerchantRegistrationInvalid is the sentinel for caller-correctable
// merchant registration input. Use errors.As with
// *MerchantRegistrationValidationError to obtain the field codes.
var ErrMerchantRegistrationInvalid = errors.New("merchant registration input is invalid")

// Stable field-error codes. Clients branch on these codes; they never carry
// submitted values.
const (
	MerchantRegistrationErrRequired = "required"
	MerchantRegistrationErrTooShort = "too_short"
	MerchantRegistrationErrTooLong  = "too_long"
	MerchantRegistrationErrInvalid  = "invalid"
	MerchantRegistrationErrTaken    = "taken"
)

// Request/storage field keys. These are the JSON keys of the HTTP contract and
// the keys of field-error maps.
const (
	MerchantRegistrationFieldLegalName         = "legal_name"
	MerchantRegistrationFieldDBAName           = "dba_name"
	MerchantRegistrationFieldTaxID             = "tax_id"
	MerchantRegistrationFieldAddressLine1      = "address_line1"
	MerchantRegistrationFieldAddressLine2      = "address_line2"
	MerchantRegistrationFieldAddressCity       = "address_city"
	MerchantRegistrationFieldAddressRegion     = "address_region"
	MerchantRegistrationFieldAddressPostalCode = "address_postal_code"
	MerchantRegistrationFieldAddressCountry    = "address_country"
	MerchantRegistrationFieldCorporateEmail    = "corporate_email"
	MerchantRegistrationFieldContactName       = "contact_name"
	MerchantRegistrationFieldPhone             = "phone"
	MerchantRegistrationFieldWebsite           = "website"
)

// Logical registration facts used for progress reporting. The registered
// address is one fact composed of several stored parts.
const (
	MerchantRegistrationFactLegalName         = "legal_name"
	MerchantRegistrationFactDBAName           = "dba_name"
	MerchantRegistrationFactTaxID             = "tax_id"
	MerchantRegistrationFactRegisteredAddress = "registered_address"
	MerchantRegistrationFactCorporateEmail    = "corporate_email"
	MerchantRegistrationFactContactName       = "contact_name"
	MerchantRegistrationFactPhone             = "phone"
	MerchantRegistrationFactWebsite           = "website"
)

// MerchantRegistrationFacts lists the eight required completion facts in
// presentation order.
var MerchantRegistrationFacts = []string{
	MerchantRegistrationFactLegalName,
	MerchantRegistrationFactDBAName,
	MerchantRegistrationFactTaxID,
	MerchantRegistrationFactRegisteredAddress,
	MerchantRegistrationFactCorporateEmail,
	MerchantRegistrationFactContactName,
	MerchantRegistrationFactPhone,
	MerchantRegistrationFactWebsite,
}

// Length bounds, counted in Unicode code points after normalization. The
// database CHECK constraints in database.go use the same bounds.
const (
	merchantRegistrationLegalNameMin     = 2
	merchantRegistrationLegalNameMax     = 200
	merchantRegistrationDBANameMin       = 2
	merchantRegistrationDBANameMax       = 120
	merchantRegistrationTaxIDMin         = 4
	merchantRegistrationTaxIDMax         = 32
	merchantRegistrationTaxIDMinAlnum    = 4
	merchantRegistrationAddressLine1Min  = 3
	merchantRegistrationAddressLineMax   = 200
	merchantRegistrationAddressLine2Min  = 1
	merchantRegistrationCityMin          = 2
	merchantRegistrationCityMax          = 100
	merchantRegistrationRegionMin        = 1
	merchantRegistrationRegionMax        = 100
	merchantRegistrationPostalMin        = 2
	merchantRegistrationPostalMax        = 16
	merchantRegistrationEmailMax         = 254
	merchantRegistrationEmailLocalMax    = 64
	merchantRegistrationContactNameMin   = 2
	merchantRegistrationContactNameMax   = 150
	merchantRegistrationWebsiteMax       = 2048
	merchantRegistrationPhonePatternText = `^\+[1-9][0-9]{9,14}$`
)

var (
	merchantRegistrationTaxIDPattern  = regexp.MustCompile(`^[A-Z0-9](?:[A-Z0-9 ./-]*[A-Z0-9])?$`)
	merchantRegistrationPostalPattern = regexp.MustCompile(`^[A-Z0-9](?:[A-Z0-9 -]*[A-Z0-9])?$`)
	merchantRegistrationPhonePattern  = regexp.MustCompile(merchantRegistrationPhonePatternText)
	merchantRegistrationEmailPattern  = regexp.MustCompile(
		"^[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@" +
			`[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?` +
			`(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$`,
	)
	merchantRegistrationWebsitePrefix = regexp.MustCompile(`(?i)^https?://`)
)

// merchantRegistrationCountryCodes is the ISO 3166-1 alpha-2 officially
// assigned code set. The Angular mirror carries the identical list.
const merchantRegistrationCountryCodeList = "" +
	"AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ " +
	"BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ " +
	"CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ " +
	"DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR " +
	"GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY " +
	"HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP " +
	"KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY " +
	"MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ " +
	"NA NC NE NF NG NI NL NO NP NR NU NZ OM " +
	"PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW " +
	"SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ " +
	"TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ " +
	"UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW"

var merchantRegistrationCountryCodes = func() map[string]struct{} {
	codes := strings.Fields(merchantRegistrationCountryCodeList)
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	return set
}()

// IsMerchantRegistrationCountryCode reports whether code is an assigned ISO
// 3166-1 alpha-2 code in canonical upper case.
func IsMerchantRegistrationCountryCode(code string) bool {
	_, ok := merchantRegistrationCountryCodes[code]
	return ok
}

// MerchantRegistrationFields carries the stored parts of the eight
// registration facts. Nil means "not supplied". After normalization every
// non-nil value is canonical and valid in format.
type MerchantRegistrationFields struct {
	LegalName         *string
	DBAName           *string
	TaxID             *string
	AddressLine1      *string
	AddressLine2      *string
	AddressCity       *string
	AddressRegion     *string
	AddressPostalCode *string
	AddressCountry    *string
	CorporateEmail    *string
	ContactName       *string
	Phone             *string
	Website           *string
}

// MerchantRegistrationFieldErrors maps a field key to a stable error code.
type MerchantRegistrationFieldErrors map[string]string

// MerchantRegistrationValidationError reports field-level failures. It wraps
// ErrMerchantRegistrationInvalid and never contains submitted values.
type MerchantRegistrationValidationError struct {
	Fields MerchantRegistrationFieldErrors
}

func (e *MerchantRegistrationValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for _, key := range merchantRegistrationFieldOrder {
		if _, ok := e.Fields[key]; ok {
			keys = append(keys, key)
		}
	}
	return "merchant registration input is invalid: " + strings.Join(keys, ", ")
}

func (e *MerchantRegistrationValidationError) Unwrap() error {
	return ErrMerchantRegistrationInvalid
}

// NewMerchantRegistrationValidationError returns nil when fields is empty.
func NewMerchantRegistrationValidationError(
	fields MerchantRegistrationFieldErrors,
) error {
	if len(fields) == 0 {
		return nil
	}
	copied := make(MerchantRegistrationFieldErrors, len(fields))
	for key, code := range fields {
		copied[key] = code
	}
	return &MerchantRegistrationValidationError{Fields: copied}
}

var merchantRegistrationFieldOrder = []string{
	MerchantRegistrationFieldLegalName,
	MerchantRegistrationFieldDBAName,
	MerchantRegistrationFieldTaxID,
	MerchantRegistrationFieldAddressLine1,
	MerchantRegistrationFieldAddressLine2,
	MerchantRegistrationFieldAddressCity,
	MerchantRegistrationFieldAddressRegion,
	MerchantRegistrationFieldAddressPostalCode,
	MerchantRegistrationFieldAddressCountry,
	MerchantRegistrationFieldCorporateEmail,
	MerchantRegistrationFieldContactName,
	MerchantRegistrationFieldPhone,
	MerchantRegistrationFieldWebsite,
}

// isMerchantRegistrationCollapsibleSpace matches exactly the JavaScript \s
// class so that the Angular mirror and this file collapse the same runes.
func isMerchantRegistrationCollapsibleSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680',
		'\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// isMerchantRegistrationForbiddenRune matches the profile rule set used across
// the platform: C0/C1 controls and bidirectional-override characters.
func isMerchantRegistrationForbiddenRune(r rune) bool {
	switch {
	case r <= '\u0008',
		r >= '\u000b' && r <= '\u001f',
		r >= '\u007f' && r <= '\u009f',
		r == '\u061c', r == '\u200e', r == '\u200f',
		r >= '\u202a' && r <= '\u202e',
		r >= '\u2066' && r <= '\u2069':
		return true
	}
	return false
}

// collapseMerchantRegistrationText trims and collapses every whitespace run to
// one ASCII space.
func collapseMerchantRegistrationText(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	pendingSpace := false
	for _, r := range raw {
		if isMerchantRegistrationCollapsibleSpace(r) {
			pendingSpace = b.Len() > 0
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func containsMerchantRegistrationForbiddenRune(value string) bool {
	for _, r := range value {
		if isMerchantRegistrationForbiddenRune(r) {
			return true
		}
	}
	return false
}

func upperASCII(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}, value)
}

func lowerASCII(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, value)
}

func hasLetterOrNumber(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

// normalizeMerchantRegistrationText validates one free-text single-line value.
func normalizeMerchantRegistrationText(raw string, minLen, maxLen int) (string, string) {
	if containsMerchantRegistrationForbiddenRune(raw) {
		return "", MerchantRegistrationErrInvalid
	}
	value := collapseMerchantRegistrationText(raw)
	if value == "" {
		return "", ""
	}
	length := utf8.RuneCountInString(value)
	if length > maxLen {
		return "", MerchantRegistrationErrTooLong
	}
	if length < minLen {
		return "", MerchantRegistrationErrTooShort
	}
	if !hasLetterOrNumber(value) {
		return "", MerchantRegistrationErrInvalid
	}
	return value, ""
}

func normalizeMerchantRegistrationTaxID(raw string) (string, string) {
	if containsMerchantRegistrationForbiddenRune(raw) {
		return "", MerchantRegistrationErrInvalid
	}
	value := upperASCII(collapseMerchantRegistrationText(raw))
	if value == "" {
		return "", ""
	}
	length := utf8.RuneCountInString(value)
	if length > merchantRegistrationTaxIDMax {
		return "", MerchantRegistrationErrTooLong
	}
	if length < merchantRegistrationTaxIDMin {
		return "", MerchantRegistrationErrTooShort
	}
	if !merchantRegistrationTaxIDPattern.MatchString(value) {
		return "", MerchantRegistrationErrInvalid
	}
	alnum := 0
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			alnum++
		}
	}
	if alnum < merchantRegistrationTaxIDMinAlnum {
		return "", MerchantRegistrationErrInvalid
	}
	return value, ""
}

func normalizeMerchantRegistrationPostalCode(raw string) (string, string) {
	if containsMerchantRegistrationForbiddenRune(raw) {
		return "", MerchantRegistrationErrInvalid
	}
	value := upperASCII(collapseMerchantRegistrationText(raw))
	if value == "" {
		return "", ""
	}
	length := utf8.RuneCountInString(value)
	if length > merchantRegistrationPostalMax {
		return "", MerchantRegistrationErrTooLong
	}
	if length < merchantRegistrationPostalMin {
		return "", MerchantRegistrationErrTooShort
	}
	if !merchantRegistrationPostalPattern.MatchString(value) {
		return "", MerchantRegistrationErrInvalid
	}
	return value, ""
}

func normalizeMerchantRegistrationCountry(raw string) (string, string) {
	value := upperASCII(collapseMerchantRegistrationText(raw))
	if value == "" {
		return "", ""
	}
	if !IsMerchantRegistrationCountryCode(value) {
		return "", MerchantRegistrationErrInvalid
	}
	return value, ""
}

func normalizeMerchantRegistrationEmail(raw string) (string, string) {
	value := lowerASCII(collapseMerchantRegistrationText(raw))
	if value == "" {
		return "", ""
	}
	if utf8.RuneCountInString(value) > merchantRegistrationEmailMax {
		return "", MerchantRegistrationErrTooLong
	}
	if !merchantRegistrationEmailPattern.MatchString(value) {
		return "", MerchantRegistrationErrInvalid
	}
	if at := strings.LastIndexByte(value, '@'); at > merchantRegistrationEmailLocalMax || personalMerchantMailbox(value[at+1:]) {
		return "", MerchantRegistrationErrInvalid
	}
	return value, ""
}

// normalizeMerchantRegistrationPhone accepts common visual separators and
// requires the platform E.164 form used by user_profiles.phone.
func normalizeMerchantRegistrationPhone(raw string) (string, string) {
	collapsed := collapseMerchantRegistrationText(raw)
	if collapsed == "" {
		return "", ""
	}
	value := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '(', ')', '.':
			return -1
		}
		return r
	}, collapsed)
	if !merchantRegistrationPhonePattern.MatchString(value) {
		return "", MerchantRegistrationErrInvalid
	}
	return value, ""
}

// normalizeMerchantRegistrationWebsite validates a private registration
// website. It applies the shared transport-safety validator (validateHTTPURL)
// and additionally requires an explicit http(s):// prefix, no whitespace or
// backslashes, and a dotted public host name.
func normalizeMerchantRegistrationWebsite(raw string) (string, string) {
	value := strings.TrimFunc(raw, isMerchantRegistrationCollapsibleSpace)
	if value == "" {
		return "", ""
	}
	if utf8.RuneCountInString(value) > merchantRegistrationWebsiteMax {
		return "", MerchantRegistrationErrTooLong
	}
	if !merchantRegistrationWebsitePrefix.MatchString(value) ||
		strings.ContainsRune(value, '\\') ||
		strings.IndexFunc(value, func(r rune) bool {
			return isMerchantRegistrationCollapsibleSpace(r) || isMerchantRegistrationForbiddenRune(r)
		}) >= 0 {
		return "", MerchantRegistrationErrInvalid
	}
	canonical, err := validateHTTPURL(value)
	if err != nil {
		return "", MerchantRegistrationErrInvalid
	}
	parsed, err := url.Parse(canonical)
	if err != nil {
		return "", MerchantRegistrationErrInvalid
	}
	host := parsed.Hostname()
	if !strings.Contains(host, ".") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", MerchantRegistrationErrInvalid
	}
	if utf8.RuneCountInString(canonical) > merchantRegistrationWebsiteMax {
		return "", MerchantRegistrationErrTooLong
	}
	return canonical, ""
}

// Corporate addresses must use a business domain. This rejects common
// personal mailbox domains; it is syntax/policy validation, not a claim of
// mailbox ownership or independent business verification.
func personalMerchantMailbox(domain string) bool {
 switch domain {
 case "gmail.com","googlemail.com","yahoo.com","ymail.com","outlook.com",
 "hotmail.com","live.com","msn.com","aol.com","icloud.com","me.com",
 "proton.me","protonmail.com","gmx.com","mail.com": return true
 }
 return false
}
var merchantPOBoxPattern = regexp.MustCompile(`(?i)\b(?:p\s*\.?\s*o\s*\.?\s*(?:box|b)|post\s+office\s+box|postal\s+box)\b`)
func normalizeMerchantRegisteredStreet(raw string) (string,string) {
 value,code:=normalizeMerchantRegistrationText(raw,merchantRegistrationAddressLine1Min,merchantRegistrationAddressLineMax)
 if code=="" && merchantPOBoxPattern.MatchString(value) { return "",MerchantRegistrationErrInvalid }
 return value,code
}

// NormalizeMerchantRegistrationFields validates the format of every supplied
// value and returns canonical values. Blank values normalize to nil. No field
// is required here; this is the draft rule set.
func NormalizeMerchantRegistrationFields(
	in MerchantRegistrationFields,
) (MerchantRegistrationFields, error) {
	var out MerchantRegistrationFields
	fieldErrors := MerchantRegistrationFieldErrors{}

	apply := func(key string, raw *string, target **string, normalize func(string) (string, string)) {
		if raw == nil {
			return
		}
		value, code := normalize(*raw)
		if code != "" {
			fieldErrors[key] = code
			return
		}
		if value != "" {
			v := value
			*target = &v
		}
	}

	text := func(minLen, maxLen int) func(string) (string, string) {
		return func(raw string) (string, string) {
			return normalizeMerchantRegistrationText(raw, minLen, maxLen)
		}
	}

	apply(MerchantRegistrationFieldLegalName, in.LegalName, &out.LegalName,
		text(merchantRegistrationLegalNameMin, merchantRegistrationLegalNameMax))
	apply(MerchantRegistrationFieldDBAName, in.DBAName, &out.DBAName,
		text(merchantRegistrationDBANameMin, merchantRegistrationDBANameMax))
	apply(MerchantRegistrationFieldTaxID, in.TaxID, &out.TaxID,
		normalizeMerchantRegistrationTaxID)
	apply(MerchantRegistrationFieldAddressLine1, in.AddressLine1, &out.AddressLine1,
		normalizeMerchantRegisteredStreet)
	apply(MerchantRegistrationFieldAddressLine2, in.AddressLine2, &out.AddressLine2,
		text(merchantRegistrationAddressLine2Min, merchantRegistrationAddressLineMax))
	apply(MerchantRegistrationFieldAddressCity, in.AddressCity, &out.AddressCity,
		text(merchantRegistrationCityMin, merchantRegistrationCityMax))
	apply(MerchantRegistrationFieldAddressRegion, in.AddressRegion, &out.AddressRegion,
		text(merchantRegistrationRegionMin, merchantRegistrationRegionMax))
	apply(MerchantRegistrationFieldAddressPostalCode, in.AddressPostalCode, &out.AddressPostalCode,
		normalizeMerchantRegistrationPostalCode)
	apply(MerchantRegistrationFieldAddressCountry, in.AddressCountry, &out.AddressCountry,
		normalizeMerchantRegistrationCountry)
	apply(MerchantRegistrationFieldCorporateEmail, in.CorporateEmail, &out.CorporateEmail,
		normalizeMerchantRegistrationEmail)
	apply(MerchantRegistrationFieldContactName, in.ContactName, &out.ContactName,
		text(merchantRegistrationContactNameMin, merchantRegistrationContactNameMax))
	apply(MerchantRegistrationFieldPhone, in.Phone, &out.Phone,
		normalizeMerchantRegistrationPhone)
	apply(MerchantRegistrationFieldWebsite, in.Website, &out.Website,
		normalizeMerchantRegistrationWebsite)

	if err := NewMerchantRegistrationValidationError(fieldErrors); err != nil {
		return MerchantRegistrationFields{}, err
	}
	return out, nil
}

// MissingMerchantRegistrationFields returns "required" for every stored part
// that completion requires but fields does not carry. fields must already be
// normalized. Address line 2, region, and postal code are optional parts of
// the registered address because not every jurisdiction uses them.
func MissingMerchantRegistrationFields(
	fields MerchantRegistrationFields,
) MerchantRegistrationFieldErrors {
	missing := MerchantRegistrationFieldErrors{}
	require := func(key string, value *string) {
		if value == nil || *value == "" {
			missing[key] = MerchantRegistrationErrRequired
		}
	}
	require(MerchantRegistrationFieldLegalName, fields.LegalName)
	require(MerchantRegistrationFieldDBAName, fields.DBAName)
	require(MerchantRegistrationFieldTaxID, fields.TaxID)
	require(MerchantRegistrationFieldAddressLine1, fields.AddressLine1)
	require(MerchantRegistrationFieldAddressCity, fields.AddressCity)
	require(MerchantRegistrationFieldAddressCountry, fields.AddressCountry)
	require(MerchantRegistrationFieldCorporateEmail, fields.CorporateEmail)
	require(MerchantRegistrationFieldContactName, fields.ContactName)
	require(MerchantRegistrationFieldPhone, fields.Phone)
	require(MerchantRegistrationFieldWebsite, fields.Website)
	return missing
}

// ValidateMerchantRegistrationForCompletion applies the completion rule set:
// the draft format rules plus every required fact. It returns canonical
// values.
func ValidateMerchantRegistrationForCompletion(
	in MerchantRegistrationFields,
) (MerchantRegistrationFields, error) {
	normalized, err := NormalizeMerchantRegistrationFields(in)
	if err != nil {
		return MerchantRegistrationFields{}, err
	}
	if err := NewMerchantRegistrationValidationError(
		MissingMerchantRegistrationFields(normalized),
	); err != nil {
		return MerchantRegistrationFields{}, err
	}
	return normalized, nil
}

// MerchantRegistrationProgress reports completion of the eight logical facts.
type MerchantRegistrationProgress struct {
	CompletedFacts int      `json:"completed_facts"`
	TotalFacts     int      `json:"total_facts"`
	MissingFacts   []string `json:"missing_facts"`
}

// AssessMerchantRegistrationProgress measures normalized fields against the
// completion rule set.
func AssessMerchantRegistrationProgress(
	fields MerchantRegistrationFields,
) MerchantRegistrationProgress {
	missing := MissingMerchantRegistrationFields(fields)
	factMissing := map[string]bool{
		MerchantRegistrationFactLegalName:      missing[MerchantRegistrationFieldLegalName] != "",
		MerchantRegistrationFactDBAName:        missing[MerchantRegistrationFieldDBAName] != "",
		MerchantRegistrationFactTaxID:          missing[MerchantRegistrationFieldTaxID] != "",
		MerchantRegistrationFactCorporateEmail: missing[MerchantRegistrationFieldCorporateEmail] != "",
		MerchantRegistrationFactContactName:    missing[MerchantRegistrationFieldContactName] != "",
		MerchantRegistrationFactPhone:          missing[MerchantRegistrationFieldPhone] != "",
		MerchantRegistrationFactWebsite:        missing[MerchantRegistrationFieldWebsite] != "",
		MerchantRegistrationFactRegisteredAddress: missing[MerchantRegistrationFieldAddressLine1] != "" ||
			missing[MerchantRegistrationFieldAddressCity] != "" ||
			missing[MerchantRegistrationFieldAddressCountry] != "",
	}

	progress := MerchantRegistrationProgress{
		TotalFacts:   len(MerchantRegistrationFacts),
		MissingFacts: make([]string, 0, len(MerchantRegistrationFacts)),
	}
	for _, fact := range MerchantRegistrationFacts {
		if factMissing[fact] {
			progress.MissingFacts = append(progress.MissingFacts, fact)
			continue
		}
		progress.CompletedFacts++
	}
	return progress
}

func equalOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Equal reports whether two normalized field sets carry identical values.
func (f MerchantRegistrationFields) Equal(other MerchantRegistrationFields) bool {
	return equalOptionalString(f.LegalName, other.LegalName) &&
		equalOptionalString(f.DBAName, other.DBAName) &&
		equalOptionalString(f.TaxID, other.TaxID) &&
		equalOptionalString(f.AddressLine1, other.AddressLine1) &&
		equalOptionalString(f.AddressLine2, other.AddressLine2) &&
		equalOptionalString(f.AddressCity, other.AddressCity) &&
		equalOptionalString(f.AddressRegion, other.AddressRegion) &&
		equalOptionalString(f.AddressPostalCode, other.AddressPostalCode) &&
		equalOptionalString(f.AddressCountry, other.AddressCountry) &&
		equalOptionalString(f.CorporateEmail, other.CorporateEmail) &&
		equalOptionalString(f.ContactName, other.ContactName) &&
		equalOptionalString(f.Phone, other.Phone) &&
		equalOptionalString(f.Website, other.Website)
}
