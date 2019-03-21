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
	"strconv"
	"strings"
	"time"

	"github.com/Outtalinenomad/slackerr"

	"github.com/OuttaLineNomad/skuvault"
	"github.com/OuttaLineNomad/skuvault/inventory"
)

var (
	// logs
	stdLog = log.New(os.Stdout, "FBAStock: ", 0)
	errLog = log.New(os.Stderr, "FBAStock Error: ", 0)
	l      = stdLog.Println

	slackHook = os.Getenv("SLACK_HOOK")
	attach    = []slackerr.Attachments{slackerr.Attachments{
		AuthorName: "FBA Stock",
		Color:      "danger",
	},
	}
	msg = &slackerr.SendMsg{
		Text:        "<@mullen.bryce> messge from FBA Stock",
		Attachments: attach,
	}
)

type locz map[string]string

type item struct {
	SKU      string
	UPC      string
	Qt       int
	Title    string
	Location string
}

type order map[string]item

type publishRequest struct {
	Orders map[string]order
}

type orders struct {
	OldOrder map[string]order
	NewOrder map[string]order
	SSOrders []ssOrder
}

type apiRespond struct {
	NewOrder map[string]order
}

// Order sends payload recived to shipstaion
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
	key, found := os.LookupEnv("SHIP_API_KEY")
	if !found {
		return errors.New("missing SHIP_API_KEY")
	}

	secret, found := os.LookupEnv("SHIP_API_SECRET")
	if !found {
		return errors.New("missing SHIP_API_SECRET")
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
	req.Header.Add("Content-Type", "application/json")
	cl := http.Client{}
	cl.Timeout = 5 * time.Minute

	resp, err := cl.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errMsg, _ := ioutil.ReadAll(resp.Body)
		errLog.Println(string(errMsg))
		return errors.New(resp.Status)
	}
	return nil
}

func (o *orders) makeOrder() error {
	brandssOrd := []ssOrder{}
	for brand, bOrd := range o.NewOrder {
		date := time.Now()
		po := "FBA-" + brand + "-" + date.Format("20060102")
		fDate := date.Format("2006-01-02T15:04:05.9999999")
		ssOr := ssOrder{
			OrderNumber: po,
			OrderDate:   fDate,
			CreateDate:  fDate,
			ModifyDate:  fDate,
			OrderStatus: "awaiting_shipment",
			BillTo: billTo{
				Name:       "Name",
				Company:    "Name",
				Country:    "US",
				Phone:      "18774484820",
				State:      "CA",
				Street1:    "1538 Howard Access Rd",
				PostalCode: "91784",
			},
			ShipTo: shipTo{
				Name:       "Name",
				Company:    "Name",
				Country:    "US",
				Phone:      "18774484820",
				State:      "CA",
				Street1:    "1538 Howard Access Rd",
				PostalCode: "91784",
			},
		}

		itmz := []ssItem{}

		for sku, ord := range bOrd {
			itm := ssItem{}
			itm.SKU = sku
			itm.UPC = ord.UPC
			itm.Quantity = ord.Qt
			itm.Name = ord.Title
			itm.ModifyDate = fDate
			itm.CreateDate = fDate
			itm.WarehouseLocation = ord.Location
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
	prod := &inventory.GetInventoryByLocation{
		PageSize:    10000,
		ProductSKUs: skus,
	}

	resp, err := sv.Inventory.GetInventoryByLocation(prod)
	if err != nil {
		return err
	}

	pub := make(map[string]order)
	pub = o.OldOrder
	for sku, svItem := range resp.Items {
		brand, err := o.getBrand(sku)
		if err != nil {
			return err
		}

		qt, loc := getTotalLocs(svItem)
		if qt == 0 {
			msg.Attachments[0].Fallback = "at of " + sku + " is zero now so will not be added to order."
			slackerr.Send(slackHook, msg, nil)
		}
		itm := item{}
		ord := make(order)
		ord = pub[brand]
		itm = ord[sku]
		if qt < itm.Qt {
			itm.Qt = qt
		}
		itm.Location = loc
		ord[sku] = itm
		pub[brand] = ord
	}

	o.NewOrder = pub

	return nil
}

func getTotalLocs(svLocs []inventory.SkuLocations) (int, string) {
	qt := 0
	location := []string{}

	for _, locs := range svLocs {
		qt += locs.Quantity
		location = append(location, locs.LocationCode+" ("+strconv.Itoa(locs.Quantity)+")")
	}

	return qt, strings.Join(location, ",")
}

func (o *orders) getBrand(sku string) (string, error) {
	for brand, ord := range o.OldOrder {
		for ordSKU := range ord {
			if sku == ordSKU {
				return brand, nil
			}
		}
	}
	return "", errors.New("cannot find brand with sku or loctiaon")
}
