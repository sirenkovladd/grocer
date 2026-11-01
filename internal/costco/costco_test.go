package costco

import (
	"strings"
	"testing"
	"time"
)

const sampleList = `{"data":{"receiptsWithCounts":{"inWarehouse":2,"gasStation":0,"carWash":0,"gasAndCarWash":0,"receipts":[{"warehouseName":"Burnaby","receiptType":"warehouse","documentType":"warehouse","transactionDateTime":"08/15/2026 14:23:11","transactionBarcode":"22055220303942610191902","transactionType":"sale","total":142.55,"totalItemCount":3,"itemArray":[{"itemNumber":"1234567"}],"tenderArray":[{"tenderTypeCode":"VI","tenderDescription":"VISA","amountTender":142.55}],"couponArray":[]},{"warehouseName":"Bellingham","receiptType":"warehouse","documentType":"warehouse","transactionDateTime":"09/02/2026 11:05:00","transactionBarcode":"22055220303942610191903","transactionType":"sale","total":58.2,"totalItemCount":1,"itemArray":[{"itemNumber":"7654321"}],"tenderArray":[],"couponArray":[]}]}}}`

const sampleDetail = `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"Burnaby","warehouseShortName":"Burnaby","receiptType":"warehouse","documentType":"warehouse","transactionDateTime":"08/15/2026 14:23:11","transactionDate":"08/15/2026","companyNumber":"1","warehouseNumber":"102","operatorNumber":"5","registerNumber":"12","transactionNumber":"34567","transactionType":"sale","transactionBarcode":"22055220303942610191902","total":142.55,"warehouseAddress1":"3550 Brighton Ave","warehouseCity":"Burnaby","warehouseState":"BC","warehouseCountry":"CA","warehousePostalCode":"V5A 3H4","totalItemCount":3,"subTotal":135.55,"taxes":7.0,"invoiceNumber":"123","sequenceNumber":"1","itemArray":[{"itemNumber":"1234567","itemDescription01":"KIRKLAND ALMOND BUTTER 1.2KG","itemDescription02":"","unit":"1","amount":18.99,"itemUnitPriceAmount":18.99},{"itemNumber":7654321,"itemDescription01":"ROTISSERIE CHICKEN","unit":"2","amount":15.98,"itemUnitPriceAmount":7.99},{"itemNumber":"9990001","itemDescription01":"","itemDescription02":"BANANAS ORGANIC","unit":"0.875","amount":1.72,"itemUnitPriceAmount":1.96}],"tenderArray":[{"tenderTypeCode":"VI","tenderDescription":"VISA","amountTender":142.55}],"instantSavings":5.0,"membershipNumber":"123456789"}]}}}`

func TestParseList(t *testing.T) {
	rows, err := ParseList([]byte(sampleList))
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].TransactionBarcode.String() != "22055220303942610191902" {
		t.Errorf("barcode = %q", rows[0].TransactionBarcode.String())
	}
	if rows[0].Total.Float64() != 142.55 {
		t.Errorf("total = %v", rows[0].Total.Float64())
	}
	if rows[0].WarehouseName != "Burnaby" {
		t.Errorf("warehouse = %q", rows[0].WarehouseName)
	}
}

func TestParseDetail(t *testing.T) {
	d, err := ParseDetail([]byte(sampleDetail))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	if d.TransactionBarcode.String() != "22055220303942610191902" {
		t.Errorf("barcode = %q", d.TransactionBarcode.String())
	}
	if len(d.ItemArray) != 3 {
		t.Fatalf("want 3 items, got %d", len(d.ItemArray))
	}
	if d.ItemArray[1].ItemNumber.String() != "7654321" {
		t.Errorf("numeric SKU not coerced: %q", d.ItemArray[1].ItemNumber.String())
	}
}

func TestConvertDetail(t *testing.T) {
	d, err := ParseDetail([]byte(sampleDetail))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	loc, _ := time.LoadLocation("America/Vancouver")
	parsed := ConvertDetail(d, loc)
	if parsed.Barcode != "22055220303942610191902" {
		t.Errorf("barcode = %q", parsed.Barcode)
	}
	if parsed.TotalCents != 14255 {
		t.Errorf("total = %d, want 14255", parsed.TotalCents)
	}
	if len(parsed.Lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(parsed.Lines))
	}
	// Line 2: qty 2 @ 7.99 = 15.98.
	if parsed.Lines[1].Quantity != 2 {
		t.Errorf("qty = %v, want 2", parsed.Lines[1].Quantity)
	}
	if parsed.Lines[1].UnitPriceCents != 799 {
		t.Errorf("unit = %d, want 799", parsed.Lines[1].UnitPriceCents)
	}
	if parsed.Lines[1].TotalPriceCents != 1598 {
		t.Errorf("total = %d, want 1598", parsed.Lines[1].TotalPriceCents)
	}
	// Line 3: fallback name from description02, weighted qty.
	if parsed.Lines[2].Name != "BANANAS ORGANIC" {
		t.Errorf("name = %q", parsed.Lines[2].Name)
	}
	if parsed.Lines[2].Quantity != 0.875 {
		t.Errorf("qty = %v, want 0.875", parsed.Lines[2].Quantity)
	}
	// Date anchored in Vancouver tz.
	if parsed.Date.Format("2006-01-02") != "2026-08-15" {
		t.Errorf("date = %s", parsed.Date.Format("2006-01-02"))
	}
}

func TestConvertIgnoresSpuriousFuelQty(t *testing.T) {
	// Real 2026-10-29 warehouse receipt: `unit` is 1 and the printed
	// amount equals the unit price, but `fuelUnitQuantity` carries a
	// stray 10.0 on non-fuel grocery lines. Quantity must resolve to 1
	// with balanced economics, not 10.
	it := DetailItem{
		ItemNumber: "1377112", ItemDescription01: "NAT PLUS 2%",
		Unit: "1", Amount: 6.89, ItemUnitPriceAmount: 6.89,
		FuelUnitQuantity: 10.0,
	}
	ln := convertLine(it)
	if ln.Quantity != 1 {
		t.Errorf("qty = %v, want 1", ln.Quantity)
	}
	if ln.UnitPriceCents != 689 {
		t.Errorf("unit = %d, want 689", ln.UnitPriceCents)
	}
	if ln.TotalPriceCents != 689 {
		t.Errorf("total = %d, want 689", ln.TotalPriceCents)
	}
}

func TestConvertWeightedLineDerivesQty(t *testing.T) {
	// Real rotisserie-thighs line: `unit` is a vacuous "1" but the
	// economics say 2.29 lb @ $15.89 = $36.39. The derived qty must win
	// so the line balances; the stray fuelUnitQuantity must not.
	it := DetailItem{
		ItemNumber: "55506", ItemDescription01: "B/S THIGHS",
		Unit: "1", Amount: 36.39, ItemUnitPriceAmount: 15.89,
		FuelUnitQuantity: 10.0,
	}
	ln := convertLine(it)
	if ln.Quantity != 2.29 {
		t.Errorf("qty = %v, want 2.29", ln.Quantity)
	}
	if ln.TotalPriceCents != 3639 {
		t.Errorf("total = %d, want 3639", ln.TotalPriceCents)
	}
}

func TestConvertFuelLineUsesFuelQty(t *testing.T) {
	// A real fuel line (grade/uom descriptors present) still honors
	// fuelUnitQuantity: 40L @ $1.85 = $74.00.
	it := DetailItem{
		ItemNumber: "FUEL", ItemDescription01: "REGULAR GAS",
		Unit: "1", Amount: 74.00, ItemUnitPriceAmount: 1.85,
		FuelUnitQuantity: 40.0, FuelGradeCode: "REG", FuelUomCode: "L",
	}
	ln := convertLine(it)
	if ln.Quantity != 40 {
		t.Errorf("qty = %v, want 40", ln.Quantity)
	}
	if ln.TotalPriceCents != 7400 {
		t.Errorf("total = %d, want 7400", ln.TotalPriceCents)
	}
}

func TestFoldFeeIntoParent(t *testing.T) {
	raw := `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-29T17:41:00","transactionBarcode":"FOLD1","total":7.01,"subTotal":7.01,"taxes":0,"itemArray":[{"itemNumber":"1377112","itemDescription01":"NAT PLUS 2%","unit":1,"amount":6.89,"itemUnitPriceAmount":6.89},{"itemNumber":"4390","itemDescription01":"ENVIRO FEE CLN/1377112","unit":1,"amount":0.02,"itemUnitPriceAmount":0},{"itemNumber":"9481","itemDescription01":"DEPOSIT CL/1377112","unit":1,"amount":0.10,"itemUnitPriceAmount":0}]}]}}}`
	d, err := ParseDetail([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	parsed := ConvertDetail(d, time.UTC)
	if len(parsed.Lines) != 1 {
		t.Fatalf("want 1 folded line, got %d", len(parsed.Lines))
	}
	ln := parsed.Lines[0]
	if ln.Name != "NAT PLUS 2%" || ln.Quantity != 1 || ln.UnitPriceCents != 701 || ln.TotalPriceCents != 701 {
		t.Errorf("got %+v, want milk qty 1 @ 701 = 701", ln)
	}
}

func TestFoldDiscountLowersParent(t *testing.T) {
	raw := `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-29T17:41:00","transactionBarcode":"FOLD2","total":5.99,"subTotal":5.99,"taxes":0,"itemArray":[{"itemNumber":"116218","itemDescription01":"BAGELS","unit":1,"amount":7.99,"itemUnitPriceAmount":7.99},{"itemNumber":"396884","itemDescription01":"/ 116218","unit":-1,"amount":-2.00,"itemUnitPriceAmount":0}]}]}}}`
	d, err := ParseDetail([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	parsed := ConvertDetail(d, time.UTC)
	if len(parsed.Lines) != 1 {
		t.Fatalf("want 1 folded line, got %d", len(parsed.Lines))
	}
	ln := parsed.Lines[0]
	if ln.Name != "BAGELS" || ln.Quantity != 1 || ln.UnitPriceCents != 599 || ln.TotalPriceCents != 599 {
		t.Errorf("got %+v, want bagels qty 1 @ 599 = 599", ln)
	}
}

func TestFoldOrphanStaysSeparate(t *testing.T) {
	raw := `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-29T17:41:00","transactionBarcode":"FOLD3","total":6.91,"subTotal":6.91,"taxes":0,"itemArray":[{"itemNumber":"1377112","itemDescription01":"NAT PLUS 2%","unit":1,"amount":6.89,"itemUnitPriceAmount":6.89},{"itemNumber":"4390","itemDescription01":"ENVIRO FEE CLN/9999999","unit":1,"amount":0.02,"itemUnitPriceAmount":0}]}]}}}`
	d, err := ParseDetail([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	parsed := ConvertDetail(d, time.UTC)
	if len(parsed.Lines) != 2 {
		t.Fatalf("want 2 lines (orphan kept), got %d", len(parsed.Lines))
	}
	if parsed.Lines[1].Name != "ENVIRO FEE CLN/9999999" || parsed.Lines[1].TotalPriceCents != 2 {
		t.Errorf("orphan changed: %+v", parsed.Lines[1])
	}
}

func TestFoldNameFallback(t *testing.T) {
	raw := `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-29T17:41:00","transactionBarcode":"FOLD4","total":14.99,"subTotal":14.99,"taxes":0,"itemArray":[{"itemNumber":"111","itemDescription01":"CHAMP SOCKS","unit":1,"amount":19.99,"itemUnitPriceAmount":19.99},{"itemNumber":"2087244","itemDescription01":"TPD/SOCKS","unit":-1,"amount":-5.00,"itemUnitPriceAmount":0}]}]}}}`
	d, err := ParseDetail([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	parsed := ConvertDetail(d, time.UTC)
	if len(parsed.Lines) != 1 {
		t.Fatalf("want 1 folded line, got %d", len(parsed.Lines))
	}
	ln := parsed.Lines[0]
	if ln.Name != "CHAMP SOCKS" || ln.UnitPriceCents != 1499 || ln.TotalPriceCents != 1499 {
		t.Errorf("got %+v, want socks @ 1499 = 1499", ln)
	}
}

func TestFoldMultiMatchFirstWins(t *testing.T) {
	raw := `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-29T17:41:00","transactionBarcode":"FOLD5","total":27.98,"subTotal":27.98,"taxes":0,"itemArray":[{"itemNumber":"111","itemDescription01":"CHAMP SOCKS","unit":1,"amount":19.99,"itemUnitPriceAmount":19.99},{"itemNumber":"222","itemDescription01":"WOOL SOCKS","unit":1,"amount":12.99,"itemUnitPriceAmount":12.99},{"itemNumber":"2087244","itemDescription01":"TPD/SOCKS","unit":-1,"amount":-5.00,"itemUnitPriceAmount":0}]}]}}}`
	d, err := ParseDetail([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	parsed := ConvertDetail(d, time.UTC)
	if len(parsed.Lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(parsed.Lines))
	}
	if parsed.Lines[0].Name != "CHAMP SOCKS" || parsed.Lines[0].UnitPriceCents != 1499 {
		t.Errorf("discount should land on first match: %+v", parsed.Lines[0])
	}
	if parsed.Lines[1].Name != "WOOL SOCKS" || parsed.Lines[1].UnitPriceCents != 1299 {
		t.Errorf("second match untouched: %+v", parsed.Lines[1])
	}
}

func TestFoldWeightedParent(t *testing.T) {
	raw := `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-29T17:41:00","transactionBarcode":"FOLD6","total":36.99,"subTotal":36.99,"taxes":0,"itemArray":[{"itemNumber":"55506","itemDescription01":"B/S THIGHS","unit":1,"amount":36.39,"itemUnitPriceAmount":15.89},{"itemNumber":"9483","itemDescription01":"DEPOSIT CL/55506","unit":1,"amount":0.60,"itemUnitPriceAmount":0}]}]}}}`
	d, err := ParseDetail([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	parsed := ConvertDetail(d, time.UTC)
	if len(parsed.Lines) != 1 {
		t.Fatalf("want 1 folded line, got %d", len(parsed.Lines))
	}
	ln := parsed.Lines[0]
	if ln.Quantity != 2.29 || ln.UnitPriceCents != 1615 || ln.TotalPriceCents != 3699 {
		t.Errorf("got %+v, want thighs qty 2.29 @ 1615 = 3699", ln)
	}
}

func TestMoneyRounding(t *testing.T) {
	// 19.99 must be 1999¢, not 1998¢ (float truncation bug).
	if got := roundCents(19.99); got != 1999 {
		t.Errorf("roundCents(19.99) = %d, want 1999", got)
	}
	if got := roundCents(142.55); got != 14255 {
		t.Errorf("roundCents(142.55) = %d, want 14255", got)
	}
}

func TestParseCostcoDate(t *testing.T) {
	loc, _ := time.LoadLocation("America/Vancouver")
	d, err := ParseCostcoDate("08/15/2026 14:23:11", loc)
	if err != nil {
		t.Fatalf("parse datetime: %v", err)
	}
	if d.Hour() != 14 || d.Day() != 15 {
		t.Errorf("datetime = %v", d)
	}
	d2, err := ParseCostcoDate("08/15/2026", loc)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	if d2.Hour() != 12 {
		t.Errorf("date-only should anchor to noon, got %v", d2)
	}
	if d2.Format("2006-01-02") != "2026-08-15" {
		t.Errorf("date = %s", d2.Format("2006-01-02"))
	}
}

func TestExternalID(t *testing.T) {
	if got := ExternalID("  abc  "); got != "costco:abc" {
		t.Errorf("ExternalID = %q", got)
	}
}

func TestExportedQueryDocs(t *testing.T) {
	if ListQueryDoc == "" || DetailQueryDoc == "" {
		t.Fatalf("query docs must be non-empty")
	}
	if !strings.Contains(ListQueryDoc, "receiptsWithCounts") || !strings.Contains(DetailQueryDoc, "receiptsWithCounts") {
		t.Fatalf("query docs must contain receiptsWithCounts")
	}
	if WCSClientID == "" || ClientIdentifier == "" {
		t.Fatalf("header IDs must be non-empty")
	}
}

func TestParseMultiDetailArray(t *testing.T) {
	raw := []byte("[" + sampleDetail + "," + sampleDetail + "]")
	out, err := ParseMultiDetail(raw)
	if err != nil {
		t.Fatalf("ParseMultiDetail: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
}
