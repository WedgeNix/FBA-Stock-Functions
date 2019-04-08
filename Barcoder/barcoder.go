package barcoder

import (
	"bufio"
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

	"github.com/jung-kurt/gofpdf/contrib/barcode"

	"github.com/jung-kurt/gofpdf"
)

var (
	// logs for Google Cloud Functions.
	stdLog = log.New(os.Stdout, "FBAStock: ", 0)
	errLog = log.New(os.Stderr, "FBAStock Error: ", 0)
	logP   = stdLog.Println
)

type publishRequest struct {
	Codes []string
	Print bool
	Title string
}

type coding struct {
	Codes []string
}

type returnAPI struct {
	Contnet string
	Print   bool
	Message string
}

type postPrintNode struct {
	PrinterID   int    `json:"printerId"`
	Title       string `json:"title"`
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
	Source      string `json:"source"`
}

// Barcoder takes data sends back printable barcodes or prints to prinnode.
func Barcoder(w http.ResponseWriter, r *http.Request) {
	authRequest(w, r)
	logP("Starting barcoder")
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
	logP("Making Barcodes and pdf")
	j, err := makeBarcodes(p.Codes, p.Title, p.Print)
	if err != nil {
		errLog.Println("makeBarcodes:", err)
		http.Error(w, "Error making PDF", http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(&j)
	logP("sent!")
}

func authRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	pass, ok := os.LookupEnv("PASS")
	if !ok {
		http.Error(w, "can't find PASS env", http.StatusInternalServerError)
		return
	}
	user, ok := os.LookupEnv("USER")
	if !ok {
		http.Error(w, "can't find USER env", http.StatusInternalServerError)
		return
	}

	tokenID := strings.Split(r.Header.Get("Authorization"), "Basic ")[1]
	envTokent := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	if tokenID != envTokent {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	return
}

func makeBarcodes(codes []string, title string, print bool) (returnAPI, error) {
	ret := returnAPI{}

	initOp := &gofpdf.InitType{
		Size: gofpdf.SizeType{
			Ht: 1.5,
			Wd: 3,
		},
		UnitStr: "in",
	}

	pdf := gofpdf.NewCustom(initOp)
	for _, code := range codes {
		err := addCode(code, pdf)
		if err != nil {
			return ret, err
		}
	}

	buf := bytes.Buffer{}
	err := pdf.Output(&buf)
	if err != nil {
		return ret, err
	}

	read := bufio.NewReader(&buf)
	b, err := ioutil.ReadAll(read)
	if err != nil {
		return ret, err
	}
	println(len(b))
	pdfBase64Str := base64.StdEncoding.EncodeToString(b)
	if print {
		err := sendToPrintNode(pdfBase64Str, title)
		if err != nil {
			return ret, err
		}

		ret.Print = print
		ret.Message = "Print successful."
		return ret, nil
	}
	ret.Print = print
	ret.Message = "Base64 pdf string sent."
	ret.Contnet = pdfBase64Str
	return ret, nil
}

// addCode adds code to pdf.
func addCode(code string, pdf *gofpdf.Fpdf) error {
	pdf.AddPage()
	pdf.SetFont("Arial", "", 18)
	pdf.CellFormat(0, -.2, code, "", 0, "C", false, 0, "")

	key := barcode.RegisterCode128(pdf, code)
	width := 3.0
	height := 1.0
	barcode.BarcodeUnscalable(pdf, key, 0, 0.4, &width, &height, false)
	return nil
}

func sendToPrintNode(pdfStr, title string) error {
	key := os.Getenv("PRINT_API_KEY")
	secret := os.Getenv("PRINT_API_SECRET")

	ok := postPrintNode{
		PrinterID:   434362,
		Title:       title,
		ContentType: "pdf_base64",
		Content:     pdfStr,
		Source:      "FBA-Stock print processing FBA",
	}

	b, err := json.Marshal(ok)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "https://api.printnode.com/printjobs", bytes.NewReader(b))
	req.Header.Add("Content-Type", "application/json")
	req.SetBasicAuth(key, secret)

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
