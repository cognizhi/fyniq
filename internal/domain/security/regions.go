package security

import "strings"

// worldRegions are the UN M49 continental regions of ISO 3166-1 alpha-2 country codes: the coarse "region" of the
// impossible-travel rule (design D7), a static table of countries, never a location database. [verify the table against
// the UN Statistics Division's M49 list]
var worldRegions = map[string]string{}

func init() {
	for region, codes := range map[string]string{
		"Africa": "DZ AO BJ BW BF BI CV CM CF TD KM CG CD CI DJ EG GQ ER SZ ET GA GM GH GN GW KE LS LR LY MG MW ML MR MU YT MA MZ NA NE NG RE " +
			"RW SH ST SN SC SL SO ZA SS SD TZ TG TN UG EH ZM ZW IO TF",
		"Americas": "AI AG AR AW BS BB BZ BM BO BQ BV BR CA KY CL CO CR CU CW DM DO EC SV FK GF GL GD GP GT GY HT HN JM MQ MX MS NI PA PY PE " +
			"PR BL KN LC MF PM VC SX GS SR TT TC US UM UY VE VG VI",
		"Asia": "AF AM AZ BH BD BT BN KH CN CY GE HK IN ID IR IQ IL JP JO KZ KP KR KW KG LA LB MO MY MV MN MM NP OM PK PS PH QA SA SG LK SY " +
			"TW TJ TH TL TR TM AE UZ VN YE",
		"Europe": "AX AL AD AT BY BE BA BG HR CZ DK EE FO FI FR DE GI GR GG VA HU IS IE IM IT JE XK LV LI LT LU MT MD MC ME NL MK NO PL PT " +
			"RO RU SM RS SK SI ES SJ SE CH UA GB",
		"Oceania":    "AS AU CX CC CK FJ PF GU HM KI MH FM NR NC NZ NU NF MP PW PG PN WS SB TK TO TV VU WF",
		"Antarctica": "AQ",
	} {
		for _, c := range strings.Fields(codes) {
			worldRegions[c] = region
		}
	}
}

// WorldRegion is the continental region of a country, or "" for a code the table does not hold.
func WorldRegion(country string) string { return worldRegions[strings.ToUpper(country)] }
