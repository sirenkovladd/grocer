package costco

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// FlexFloat accepts a JSON number or a quoted numeric string ("12.99").
// Costco's ecom API mixes the two across fields and locales, so every
// money/quantity field uses this type.
type FlexFloat float64

func (f *FlexFloat) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var inner string
		if err := json.Unmarshal(data, &inner); err != nil {
			return err
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			*f = 0
			return nil
		}
		// Strip currency symbols, commas, whitespace: "$1,234.56" -> "1234.56".
		cleaned := strings.Map(func(r rune) rune {
			if (r >= '0' && r <= '9') || r == '.' || r == '-' {
				return r
			}
			return -1
		}, inner)
		if cleaned == "" || cleaned == "-" || cleaned == "." {
			*f = 0
			return nil
		}
		v, err := strconv.ParseFloat(cleaned, 64)
		if err != nil {
			return fmt.Errorf("flexfloat %q: %w", inner, err)
		}
		*f = FlexFloat(v)
		return nil
	}
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		// Last resort: try string-encoded number without quotes handled above.
		var s2 string
		if err2 := json.Unmarshal(data, &s2); err2 == nil {
			v2, err3 := strconv.ParseFloat(strings.TrimSpace(s2), 64)
			if err3 != nil {
				return err
			}
			*f = FlexFloat(v2)
			return nil
		}
		return err
	}
	*f = FlexFloat(v)
	return nil
}

func (f FlexFloat) Float64() float64 { return float64(f) }

// FlexString accepts a JSON string or number and always yields a string.
// Costco item numbers are numeric in some responses, strings in others.
type FlexString string

func (s *FlexString) UnmarshalJSON(data []byte) error {
	t := strings.TrimSpace(string(data))
	if t == "" || t == "null" {
		*s = ""
		return nil
	}
	if len(t) >= 2 && t[0] == '"' && t[len(t)-1] == '"' {
		var inner string
		if err := json.Unmarshal(data, &inner); err != nil {
			return err
		}
		*s = FlexString(inner)
		return nil
	}
	// Bare number -> stringify without losing precision for barcodes.
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f == float64(int64(f)) {
		*s = FlexString(strconv.FormatInt(int64(f), 10))
		return nil
	}
	*s = FlexString(strconv.FormatFloat(f, 'f', -1, 64))
	return nil
}

func (s FlexString) String() string { return string(s) }

// ---- List query ----------------------------------------------------------

// ListResponse is the top-level envelope for the receiptsWithCounts
// date-range query. The data path is data.receiptsWithCounts.
type ListResponse struct {
	Data struct {
		ReceiptsWithCounts struct {
			InWarehouse   int          `json:"inWarehouse"`
			GasStation    int          `json:"gasStation"`
			CarWash       int          `json:"carWash"`
			GasAndCarWash int          `json:"gasAndCarWash"`
			Receipts      []ListReceipt `json:"receipts"`
		} `json:"receiptsWithCounts"`
	} `json:"data"`
}

// ListReceipt is one row of the list view.
type ListReceipt struct {
	WarehouseName      string     `json:"warehouseName"`
	ReceiptType        FlexString `json:"receiptType"`
	DocumentType       FlexString `json:"documentType"`
	TransactionDateTime FlexString `json:"transactionDateTime"`
	TransactionBarcode FlexString `json:"transactionBarcode"`
	TransactionType    FlexString `json:"transactionType"`
	Total              FlexFloat  `json:"total"`
	TotalItemCount     int        `json:"totalItemCount"`
	ItemArray          []struct {
		ItemNumber FlexString `json:"itemNumber"`
	} `json:"itemArray"`
	TenderArray []struct {
		TenderTypeCode  FlexString `json:"tenderTypeCode"`
		TenderDescription FlexString `json:"tenderDescription"`
		AmountTender    FlexFloat  `json:"amountTender"`
	} `json:"tenderArray"`
	CouponArray []struct {
		UPC FlexString `json:"upcnumberCoupon"`
	} `json:"couponArray"`
}

// ---- Detail query --------------------------------------------------------

// DetailResponse is the envelope for the single-barcode query.
type DetailResponse struct {
	Data struct {
		ReceiptsWithCounts struct {
			Receipts []DetailReceipt `json:"receipts"`
		} `json:"receiptsWithCounts"`
	} `json:"data"`
}

// DetailReceipt is the full breakdown of one warehouse visit.
type DetailReceipt struct {
	WarehouseName      string     `json:"warehouseName"`
	WarehouseShortName FlexString `json:"warehouseShortName"`
	ReceiptType        FlexString `json:"receiptType"`
	DocumentType       FlexString `json:"documentType"`
	TransactionDateTime FlexString `json:"transactionDateTime"`
	TransactionDate    FlexString `json:"transactionDate"`
	CompanyNumber      FlexString `json:"companyNumber"`
	WarehouseNumber    FlexString `json:"warehouseNumber"`
	OperatorNumber     FlexString `json:"operatorNumber"`
	RegisterNumber     FlexString `json:"registerNumber"`
	TransactionNumber  FlexString `json:"transactionNumber"`
	TransactionType    FlexString `json:"transactionType"`
	TransactionBarcode FlexString `json:"transactionBarcode"`
	Total              FlexFloat  `json:"total"`
	SubTotal           FlexFloat  `json:"subTotal"`
	Taxes              FlexFloat  `json:"taxes"`
	TotalItemCount     int        `json:"totalItemCount"`
	WarehouseAddress1  string     `json:"warehouseAddress1"`
	WarehouseAddress2  string     `json:"warehouseAddress2"`
	WarehouseCity      string     `json:"warehouseCity"`
	WarehouseState     string     `json:"warehouseState"`
	WarehouseCountry   string     `json:"warehouseCountry"`
	WarehousePostal    string     `json:"warehousePostalCode"`
	InvoiceNumber      FlexString `json:"invoiceNumber"`
	SequenceNumber     FlexString `json:"sequenceNumber"`
	InstantSavings     FlexFloat  `json:"instantSavings"`
	MembershipNumber   FlexString `json:"membershipNumber"`
	ItemArray          []DetailItem `json:"itemArray"`
	TenderArray        []DetailTender `json:"tenderArray"`
}

// DetailItem is one line on a Costco receipt.
type DetailItem struct {
	ItemNumber           FlexString `json:"itemNumber"`
	ItemDescription01    string     `json:"itemDescription01"`
	FrenchItemDesc1      string     `json:"frenchItemDescription1"`
	ItemDescription02    string     `json:"itemDescription02"`
	FrenchItemDesc2      string     `json:"frenchItemDescription2"`
	ItemIdentifier       FlexString `json:"itemIdentifier"`
	ItemDepartmentNumber FlexString `json:"itemDepartmentNumber"`
	Unit                 FlexString `json:"unit"`
	Amount               FlexFloat  `json:"amount"`
	TaxFlag              FlexString `json:"taxFlag"`
	MerchantID           FlexString `json:"merchantID"`
	EntryMethod          FlexString `json:"entryMethod"`
	TransDepartmentNumber FlexString `json:"transDepartmentNumber"`
	// Weighted / fuel extensions. Only one family is populated per row.
	FuelUnitQuantity     FlexFloat  `json:"fuelUnitQuantity"`
	FuelGradeCode        FlexString `json:"fuelGradeCode"`
	ItemUnitPriceAmount  FlexFloat  `json:"itemUnitPriceAmount"`
	FuelUomCode          FlexString `json:"fuelUomCode"`
	FuelUomDescription   string     `json:"fuelUomDescription"`
	FuelGradeDescription string     `json:"fuelGradeDescription"`
}

// DetailTender is one payment line.
type DetailTender struct {
	TenderTypeCode   FlexString `json:"tenderTypeCode"`
	TenderSubTypeCode FlexString `json:"tenderSubTypeCode"`
	TenderDescription string    `json:"tenderDescription"`
	AmountTender     FlexFloat  `json:"amountTender"`
	DisplayAccount   FlexString `json:"displayAccountNumber"`
	TenderTypeName   FlexString `json:"tenderTypeName"`
}
