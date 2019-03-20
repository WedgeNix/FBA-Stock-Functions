package order

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/OuttaLineNomad/skuvault/products"

	"github.com/OuttaLineNomad/skuvault"
)

var (
	// logs
	stdLog = log.New(os.Stdout, "FBAStock: ", 0)
	errLog = log.New(os.Stderr, "FBAStock Error: ", 0)
	l      = stdLog.Println
)

type item struct {
	SKU   string
	UPC   string
	Qt    int
	Title string
}

type order map[string]item

type publishRequest struct {
	Orders map[string]order
}

type orders struct {
	OldOrder map[string]order
	NewOrder map[string]order
	SSOrders []SsOrder
}

type apiRespond struct {
	NewOrder map[string]order
}

func Order(w http.ResponseWriter, r *http.Request) {
	if err := authRequest(r); err != nil {
		errLog.Println("authRequest:", err)
		http.Error(w, "Error authorizing request", http.StatusUnauthorized)
	}

	// Read the request body.
	req, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errLog.Println("iouitl.ReadAll:", err)
		http.Error(w, "Error reading request", http.StatusBadRequest)
		return
	}

	// Parse json into struct
	p := publishRequest{}
	if err := json.Unmarshal(req, &p); err != nil {
		errLog.Println("json.Unmarshal:", err)
		http.Error(w, "Error parsing request", http.StatusBadRequest)
		return
	}

	ordrz := orders{}
	ordrz.OldOrder = p.Orders

	err = ordrz.matchQt()
	if err != nil {
		errLog.Println("matchQt:", err)
		http.Error(w, "Server error matching quantities", http.StatusInternalServerError)
		return
	}
	l("done with machQt now makeing SS order")

	err = ordrz.makeOrder()
	if err != nil {
		errLog.Println("makeOrder:", err)
		http.Error(w, "Server error makeing order", http.StatusInternalServerError)
		return
	}
	l("done with making orders now sending to ShipStaion...")

	err = ordrz.send()
	if err != nil {
		errLog.Println("send:", err)
		http.Error(w, "Server error sending order", http.StatusInternalServerError)
		return
	}
	l("done sending to ShipStaion...")

	newResp := apiRespond{
		NewOrder: ordrz.NewOrder,
	}

	json.NewEncoder(w).Encode(&newResp)
	l("sent!")

}

func authRequest(r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("wrong mthod")
	}

	pass, ok := os.LookupEnv("PASS")
	if !ok {
		return errors.New("can't find PASS env")
	}
	user, ok := os.LookupEnv("USER")
	if !ok {
		return errors.New("can't find USER env")
	}

	tokenID := strings.Split(r.Header.Get("Authorization"), "Basic ")[1]
	envTokent := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	if tokenID != envTokent {
		return errors.New("authorization is denied")
	}
	return nil
}

func (o *orders) send() error {
	key, found := os.LookupEnv("SHIPSTATION_KEY")
	if !found {
		return errors.New("missing SHIPSTATION_KEY")
	}
	secret, found := os.LookupEnv("SHIPSTATION_SECRET")
	if !found {
		return errors.New("missing SHIPSTATION_SECRET")
	}

	b, err := json.Marshal(o.SSOrders)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "https://ssapi.shipstation.com/orders/createorders", bytes.NewBuffer(b))
	if err != nil {
		return err
	}

	req.SetBasicAuth(key, secret)

	cl := http.Client{}
	cl.Timeout = 5 * time.Minute

	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	return nil
}

func (o *orders) makeOrder() error {

	brandssOrd := []SsOrder{}
	for brand, bOrd := range o.NewOrder {
		date := time.Now()
		po := "FBA-" + brand + "-" + date.Format("20060102")
		ssOr := SsOrder{
			OrderNumber: po,
			OrderDate:   date.Format("2006-01-02T15:04:05.9999999"),
			OrderStatus: "awaiting_shipment",
			BillTo: BillTo{
				Name:       "WedgeNix",
				Company:    "WedgeNix",
				Country:    "US",
				Phone:      "909-908-6413",
				State:      "CA",
				Street1:    "1991 Windemere Ct",
				PostalCode: "91784",
			},
			ShipTo: ShipTo{
				Name:       "WedgeNix",
				Company:    "WedgeNix",
				Country:    "US",
				Phone:      "909-908-6413",
				State:      "CA",
				Street1:    "1991 Windemere Ct",
				PostalCode: "91784",
			},
		}

		itmz := []SsItem{}

		for sku, ord := range bOrd {
			itm := SsItem{}
			itm.SKU = sku
			itm.UPC = ord.UPC
			itm.Quantity = ord.Qt
			itm.Name = ord.Title
			itmz = append(itmz, itm)
		}
		ssOr.Items = itmz
		brandssOrd = append(brandssOrd, ssOr)
	}
	o.SSOrders = brandssOrd
	return nil
}

func (o *orders) matchQt() error {
	skus := []string{}
	for _, skuz := range o.OldOrder {
		for sku := range skuz {
			skus = append(skus, sku)
		}
	}

	sv := skuvault.NewEnvCredSession()
	prod := &products.GetProducts{
		PageSize:    10000,
		ProductSKUs: skus,
	}

	resp, err := sv.Products.GetProducts(prod)
	if err != nil {
		return err
	}

	pub := make(map[string]order)
	pub = o.OldOrder
	for _, svItem := range resp.Products {
		itm := item{}
		ord := make(order)
		ord = pub[svItem.Brand]
		itm = ord[svItem.Sku]
		if svItem.QuantityAvailable < itm.Qt {
			itm.Qt = svItem.QuantityAvailable
			ord[svItem.Sku] = itm
		}
		pub[svItem.Brand] = ord
	}

	o.NewOrder = pub

	return nil
}
