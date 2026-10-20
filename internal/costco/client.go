package costco

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Direct-fetch GraphQL documents. They mirror the browser queries from
// DevTools so server-side results have the same shape as pasted ones
// and flow through the same Parse/Convert path.
const listQuery = `query receiptsWithCounts($startDate: String!, $endDate: String!,$documentType:String!,$documentSubType:String!) {
    receiptsWithCounts(startDate: $startDate, endDate: $endDate,documentType:$documentType,documentSubType:$documentSubType) {
    inWarehouse
    gasStation
    carWash
    gasAndCarWash
    receipts{
    warehouseName receiptType  documentType transactionDateTime transactionBarcode warehouseName transactionType total
    totalItemCount
    itemArray {
      itemNumber
    }
    tenderArray {
      tenderTypeCode
      tenderDescription
      amountTender
    }
    couponArray {
      upcnumberCoupon
    }
  }
}
  }`

const detailQuery = `query receiptsWithCounts($barcode: String!,$documentType:String!) {
    receiptsWithCounts(barcode: $barcode,documentType:$documentType) {
receipts{
      warehouseName
      receiptType
      documentType
      transactionDateTime
      transactionDate
      companyNumber
      warehouseNumber
      operatorNumber
      warehouseName
      warehouseShortName
      registerNumber
      transactionNumber
      transactionType
      transactionBarcode
      total
      warehouseAddress1
      warehouseAddress2
      warehouseCity
      warehouseState
      warehouseCountry
      warehousePostalCode
      totalItemCount
      subTotal
      taxes
      total
      invoiceNumber
      sequenceNumber
      itemArray {
        itemNumber
        itemDescription01
        frenchItemDescription1
        itemDescription02
        frenchItemDescription2
        itemIdentifier
        itemDepartmentNumber
        unit
        amount
        taxFlag
        merchantID
        entryMethod
        transDepartmentNumber
        fuelUnitQuantity
        fuelGradeCode
        itemUnitPriceAmount
        fuelUomCode
        fuelUomDescription
        fuelGradeDescription
      }
      tenderArray {
        tenderTypeCode
        tenderSubTypeCode
        tenderDescription
        amountTender
        displayAccountNumber
        sequenceNumber
        tenderTypeName
         transactionID
      }
        subTaxes {
          tax1 tax2 tax3 tax4
        }
        instantSavings
        membershipNumber
    }
  }
 }`

// FetchCredentials are the user-supplied auth headers copied from a
// logged-in Costco tab (DevTools → copy as fetch). Only Bearer is
// strictly required; the wcs client id / env / service headers improve
// the odds the ecom gateway accepts the call. Kasada headers
// (x-kpsdk-*) are deliberately NOT accepted: they are single-use
// challenge answers bound to the browser session and replaying them
// server-side is both unreliable and indistinguishable from bot abuse.
type FetchCredentials struct {
	Bearer         string `json:"bearer"`
	WCSClientID    string `json:"wcsClientId,omitempty"`
	ClientIdentifier string `json:"clientIdentifier,omitempty"`
	Cookies        string `json:"cookies,omitempty"`
}

// Client performs server-side Costco GraphQL calls on behalf of a user
// who pasted a fresh bearer token. Timeouts are tight (30s) so a hung
// Costco edge doesn't wedge the import worker.
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func (c *Client) doGraphQL(ctx context.Context, creds FetchCredentials, query string, vars map[string]any) (json.RawMessage, error) {
	if creds.Bearer == "" {
		return nil, fmt.Errorf("bearer token is required")
	}
	body, err := json.Marshal(gqlRequest{Query: query, Variables: vars})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://ecom-api.costco.com/ebusiness/order/v1/orders/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.Header.Set("costco-x-authorization", "Bearer "+creds.Bearer)
	req.Header.Set("costco.env", "ecom")
	req.Header.Set("costco.service", "restOrders")
	req.Header.Set("Referer", "https://www.costco.ca/")
	if creds.WCSClientID != "" {
		req.Header.Set("costco-x-wcs-clientid", creds.WCSClientID)
	} else {
		req.Header.Set("costco-x-wcs-clientid", "e442e6e6-2602-4a39-937b-8b28b4457ed3")
	}
	if creds.ClientIdentifier != "" {
		req.Header.Set("client-identifier", creds.ClientIdentifier)
	} else {
		req.Header.Set("client-identifier", "481b1aec-aa3b-454b-b81b-48187e28f205")
	}
	if creds.Cookies != "" {
		req.Header.Set("Cookie", creds.Cookies)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("costco request: %w", err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode costco response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("costco API %d: %.500s", resp.StatusCode, string(raw))
	}
	// Surface GraphQL errors with the body attached for debugging.
	var errEnv struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &errEnv); err == nil && len(errEnv.Errors) > 0 {
		return nil, fmt.Errorf("costco GraphQL: %s", errEnv.Errors[0].Message)
	}
	return raw, nil
}

// FetchList returns the raw list envelope for a date range. Dates use
// Costco's M/D/YYYY format ("8/01/2026").
func (c *Client) FetchList(ctx context.Context, creds FetchCredentials, startDate, endDate string) ([]ListReceipt, error) {
	raw, err := c.doGraphQL(ctx, creds, listQuery, map[string]any{
		"startDate": startDate, "endDate": endDate,
		"text": "Last 3 Months", "documentType": "all", "documentSubType": "all",
	})
	if err != nil {
		return nil, err
	}
	return ParseList(raw)
}

// FetchDetail returns the full breakdown for one barcode.
func (c *Client) FetchDetail(ctx context.Context, creds FetchCredentials, barcode string) (*DetailReceipt, error) {
	raw, err := c.doGraphQL(ctx, creds, detailQuery, map[string]any{
		"barcode": barcode, "documentType": "warehouse",
	})
	if err != nil {
		return nil, err
	}
	return ParseDetail(raw)
}
