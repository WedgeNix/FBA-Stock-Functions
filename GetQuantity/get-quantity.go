package getquantity

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/OuttaLineNomad/skuvault/inventory"

	"github.com/OuttaLineNomad/skuvault"
)

var (
	// logs for Google Cloud Functions.
	stdLog = log.New(os.Stdout, "FBAStock: ", 0)
	errLog = log.New(os.Stderr, "FBAStock Error: ", 0)
	logP   = stdLog.Println
)

type publishRequest struct {
	Codes []string
}

type response map[string]int

// GetQuantity gets SKU Vault quantity for FBA location to make order.
func GetQuantity(w http.ResponseWriter, r *http.Request) {
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

	rsp, err := getSKUQt(p.Codes)
	if err != nil {
		errLog.Println("getSKUQt:", err)
		http.Error(w, "Error getting SKU Quantity", http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(&rsp)
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

func getSKUQt(codes []string) (response, error) {
	rsp := response{}
	sv := skuvault.New()
	getItm := &inventory.GetItemQuantities{
		ProductCodes: codes,
	}

	resp, err := sv.Inventory.GetItemQuantities(getItm)
	if err != nil {
		return nil, err
	}

	for _, itm := range resp.Items {
		rsp[itm.Sku] = itm.TotalOnHand
	}

	return rsp, nil
}
