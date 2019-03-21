package stock

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/WedgeNix/excel"

	"github.com/OuttaLineNomad/skuvault"
	"github.com/OuttaLineNomad/skuvault/products"

	"github.com/OuttaLineNomad/storage"
)

var (
	caData     = regexp.MustCompile(`([Dd][Aa][Tt][Aa]|[Cc][Aa]|[Oo][Rr][Dd][Ee][Rr])[_ -]?([Dd][Aa][Tt][Aa]|[Cc][Aa]|[Oo][Rr][Dd][Ee][Rr])`)
	amzViews   = regexp.MustCompile(`[Bb]usiness[ _-]?[Rr]eport`)
	fbaRestock = regexp.MustCompile(`[Rr]estock[- _][Rr]eport`)

	sugPriceUser = false

	rules = &rulesFile{}

	delScrCH = make(chan bool)
	errCH    = make(chan error)
	// logs
	stdLog = log.New(os.Stdout, "FBAStock: ", 0)
	errLog = log.New(os.Stderr, "FBAStock Error: ", 0)
	// debug  = log.New(os.Stdout, "[debug] ", 0)

	l = stdLog.Println
	// db = debug.Println
)

type svData struct {
	Cost        float64
	Class       string
	UPC         string
	Brand       string
	AvailableQt int
	SvTitle     string
}

type svDatas map[string]svData

type fbaStockFiles struct {
	CAData     map[string]topSellerH
	FBARestock map[string]fbaRestockH
	AMZViews   map[string]amzViewsH
}

type rulesFile struct {
	Topseller int
	Profit    float64
	Fees      struct {
		FeePercentage float64
		FBAClass      map[string]float64
	}
	DaysCoverd      int
	SalesMultiplier float64
}

type fbaRestockH struct {
	fbaRestockF
	svData
}

type fbaRestockF struct {
	SKU     string
	Inbound int
	Alert   string
	RecQt   int
	RecDate time.Time
}

type amzViewsH struct {
	Parent                string
	Child                 string
	SKU                   string
	Sessions              int
	PageViews             int
	BuyBoxPercentage      float64
	UnitSessionPercentage float64
	UnitsOrdered          int
	OrdProdSales          float64
}

type top struct {
	ReportStartDate time.Time
	ReportEndDate   time.Time
	Title           string
	QtySold         int
	GMV             float64
	SKU             string
}

type topSellerH struct {
	top
	svData
	Fees     float64
	EstPrice float64
	SugPrice float64
	EstProf  float64
	SugQt    int
	Restock  bool
	amzViewsH
}

type apiRespond struct {
	Suggested  map[string]topSellerH  `json:"Suggested"`
	FBARestock map[string]fbaRestockH `json:"FBARestock"`
}

type publishRequest struct {
	SuggestPrice bool `json:"suggest_price"`
	DeleteSource bool `json:"delete_source"`
}

type brandReg map[string]*regexp.Regexp

// Stock function to get fba stock data to find out what to buy and to send to FBA.
func Stock(w http.ResponseWriter, r *http.Request) {
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
	l(string(req))
	// Parse json into struct
	p := publishRequest{}
	if err := json.Unmarshal(req, &p); err != nil {
		errLog.Println("json.Unmarshal:", err)
		http.Error(w, "Error parsing request", http.StatusBadRequest)
		return
	}

	sugPriceUser = p.SuggestPrice

	// DeleteSouce does not work because scope for drive is not working.
	if p.DeleteSource {
		errLog.Println("delete_source is future feature does not work now; no files will be deleted")
		p.DeleteSource = false
	}

	l("pulling all drive files...")
	data, err := getDrive()
	if err != nil {
		errLog.Println("getDrive:", err)
		http.Error(w, "Server error reading files", http.StatusInternalServerError)
		return
	}

	l("files successfully pulled now getting suggestions...")
	err = data.getSuggestion()
	if err != nil {
		errLog.Println("getSuggestion:", err)
		http.Error(w, "Error creating report", http.StatusInternalServerError)
		return
	}

	l("adding sugested restock...")
	delScrCH <- p.DeleteSource
	if err := <-errCH; err != nil {
		errLog.Println("deleteSource:", err)
		http.Error(w, "Error deleting source", http.StatusInternalServerError)
		return
	}

	newResp := apiRespond{
		Suggested:  data.CAData,
		FBARestock: data.FBARestock,
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

func (filz *fbaStockFiles) createReport() ([]byte, error) {
	newResp := apiRespond{
		Suggested:  filz.CAData,
		FBARestock: filz.FBARestock,
	}

	b, err := json.Marshal(newResp)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func getDrive() (*fbaStockFiles, error) {
	var fbar []fbaRestockF
	var amzv []amzViewsH
	var tops []top
	s, err := storage.NewGoogle()
	if err != nil {
		return nil, err
	}

	filz, err := s.Drive.GetFileIDs("1I0oW4tyYvZWC9a6BXh0UcymZlP61KOZh", "text/csv", "application/json")
	if err != nil {
		return nil, err
	}

	if len(filz) != 4 {
		errLog.Println("file cound:", len(filz))
		return nil, errors.New(`getDrive: not enough/to manny files - there must be 4 files in this folder`)
	}

	go deleteSource(s, filz)

	stock := &fbaStockFiles{}
	for _, id := range filz {
		if id.Name == `rules.json` {
			getSettings(id.ID, s.Drive)
			continue
		}

		csvData, err := getData(id.ID, s.Drive)
		if err != nil {
			return nil, err
		}
		f := excel.File{
			Sheets: [][][]string{csvData},
		}

		if caData.MatchString(id.Name) {
			if err = f.Unmarshal(&tops); err != nil {
				return nil, err
			}
			stock.mapDataCA(tops)
			continue
		}

		if fbaRestock.MatchString(id.Name) {
			if err = f.Unmarshal(&fbar); err != nil {
				return nil, err
			}
			stock.mapDataRestock(fbar)
			continue
		}

		if amzViews.MatchString(id.Name) {
			if err = f.Unmarshal(&amzv); err != nil {
				return nil, err
			}
			stock.mapDataView(amzv)
			continue
		}
		return nil, errors.New(`getData: filename ` + id.Name + ` in folder not recognized`)
	}

	return stock, nil
}

func deleteSource(s *storage.Storage, files []storage.DriveFile) {
	if <-delScrCH {
		l("deleating source files.")
		for _, f := range files {
			err := s.Drive.Files.Delete(f.ID).Do()
			if err != nil {
				errCH <- err
				return
			}
		}
	}
	errCH <- nil
}

func (filz *fbaStockFiles) addToFBAReStk(skus svDatas) svDatas {
	svD := make(svDatas)
	newFBA := make(map[string]fbaRestockH)
	for sku, data := range skus {
		fbaOk := fbaRestockH{}
		fba, ok := filz.FBARestock[sku]
		if ok {
			fbaOk.svData = data
			fbaOk.fbaRestockF = fba.fbaRestockF
			newFBA[sku] = fbaOk
			continue
		}
		svD[sku] = data
	}
	filz.FBARestock = newFBA
	return svD
}

func (filz *fbaStockFiles) getSuggestion() error {
	costMap, err := filz.getSvData()
	if err != nil {
		return err
	}

	for sku, svD := range costMap {
		estPrice := filz.AMZViews[sku].OrdProdSales / float64(filz.AMZViews[sku].UnitsOrdered)
		if math.IsNaN(estPrice) {
			estPrice = 0
		}
		estFees := getFees(sku, estPrice, svD.Class)
		totalCost := svD.Cost + estFees
		prof := estPrice - totalCost

		if prof < rules.Profit && !sugPriceUser || estPrice == 0.00 {
			delete(filz.CAData, sku)
			continue
		}

		var suggestedPrice float64
		if sugPriceUser {
			suggestedPrice = totalCost + rules.Profit
		}

		alert, ok := filz.FBARestock[sku]
		if estPrice == 0 && ok {
			alert.Alert += " [SKU had no sales on AMZ]"
		}

		restock := false
		test := fbaRestockH{}
		if filz.FBARestock[sku] != test {
			restock = true
		}

		sugQt := filz.getSugQt(sku, svD)
		if sugQt == 0 || sugQt == -1 {
			delete(filz.CAData, sku)
			continue
		}

		myMap := topSellerH{}
		vuMap := amzViewsH{}
		myMap = filz.CAData[sku]
		vuMap = filz.AMZViews[sku]
		myMap.amzViewsH = vuMap
		myMap.svData = svD
		myMap.SugQt = sugQt
		myMap.EstPrice = estPrice
		myMap.EstProf = prof
		myMap.Fees = estFees
		myMap.Restock = restock
		myMap.SugPrice = suggestedPrice

		filz.CAData[sku] = myMap
	}

	return nil
}

func (filz *fbaStockFiles) getSugQt(sku string, svd svData) int {
	days := filz.CAData[sku].ReportEndDate.Sub(filz.CAData[sku].ReportStartDate).Hours() / 24
	qt := int((float64(filz.CAData[sku].top.QtySold)/float64(days))*float64(rules.DaysCoverd)*rules.SalesMultiplier + 0.5)
	if qt == 1 {
		qt = 2
	}

	available := svd.AvailableQt
	if qt > available {
		qt = available
	}

	return qt
}

// getFees estimates fees for FBA.
func getFees(sku string, price float64, class string) float64 {
	fees := rules.Fees
	classFee, ok := fees.FBAClass[class]
	if !ok {
		classFee = fees.FBAClass["default"]
	}
	mrktFee := price * fees.FeePercentage
	return mrktFee + classFee
}

func (filz *fbaStockFiles) getSvData() (svDatas, error) {
	skus := []string{}
	for sku := range filz.CAData {
		skus = append(skus, sku)
	}

	for fsku := range filz.FBARestock {
		skus = append(skus, fsku)
	}

	sv := skuvault.NewEnvCredSession()

	prod := &products.GetProducts{
		PageSize:    10000,
		ProductSKUs: skus,
	}

	resp, err := sv.Products.GetProducts(prod)
	if err != nil {
		return nil, err
	}

	svD := make(svDatas)
	for _, prod := range resp.Products {
		svD[prod.Sku] = svData{prod.Cost, prod.Classification, prod.Code, prod.Brand, prod.QuantityAvailable, prod.Description}
	}

	if len(skus) != len(svD) {
		stdLog.Println(`skus are off by:`, len(skus)-len(svD))
	}

	newSVD := filz.addToFBAReStk(svD)

	return newSVD, nil
}

func getSettings(id string, d storage.DriverService) error {
	res, err := d.GetFile(id)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	err = json.NewDecoder(res.Body).Decode(rules)
	if err != nil {
		return err
	}

	return nil
}

func getData(id string, d storage.DriverService) ([][]string, error) {
	res, err := d.GetFile(id)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	c := csv.NewReader(res.Body)
	c.LazyQuotes = true
	cdata, err := c.ReadAll()
	if err != nil {
		return nil, err
	}
	return cdata, nil
}

func (filz *fbaStockFiles) mapDataCA(data []top) {
	ca := make(map[string]topSellerH)
	for _, row := range data {
		sku := row.SKU
		if row.QtySold < rules.Topseller {
			continue
		}
		ca[sku] = topSellerH{
			top: row,
		}
	}
	filz.CAData = ca
}

func (filz *fbaStockFiles) mapDataView(data []amzViewsH) {
	vu := make(map[string]amzViewsH)

	for _, row := range data {
		sku := row.SKU
		vu[sku] = row
	}

	filz.AMZViews = vu
}

func (filz *fbaStockFiles) mapDataRestock(data []fbaRestockF) {
	rs := make(map[string]fbaRestockH)

	for _, row := range data {
		new := fbaRestockH{}
		if row.Alert == "" && row.RecQt == 0 {
			continue
		}
		sku := row.SKU
		new.fbaRestockF = row
		rs[sku] = new
	}
	filz.FBARestock = rs
	return
}
