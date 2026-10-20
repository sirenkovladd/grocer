package costco

import (
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
