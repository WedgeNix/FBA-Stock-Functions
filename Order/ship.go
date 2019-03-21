package order

type ssOrder struct {
	OrderID                  int
	OrderNumber              string
	OrderKey                 string
	OrderDate                string
	CreateDate               string
	ModifyDate               string
	PaymentDate              string
	ShipByDate               string
	OrderStatus              string
	CustomerID               int
	CustomerUsername         string
	CustomerEmail            string
	BillTo                   billTo
	ShipTo                   shipTo
	Items                    []ssItem
	OrderTotal               float32
	AmountPaid               float32
	TaxAmount                float32
	ShippingAmount           float32
	CustomerNotes            string
	InternalNotes            string
	Gift                     bool
	GiftMessage              string
	PaymentMethod            string
	RequestedShippingService string
	CarrierCode              string
	ServiceCode              string
	PackageCode              string
	Confirmation             string
	ShipDate                 string
	HoldUntilDate            string
	Weight                   interface{}
	Dimensions               interface{}
	InsuranceOptions         interface{}
	InternationalOptions     interface{}
	AdvancedOptions          advancedOptions
	TagIDs                   []int
	UserID                   string
	ExternallyFulfilled      bool
	ExternallyFulfilledBy    string
}

type ssItem struct {
	OrderItemID       int
	LineItemKey       string
	SKU               string
	Name              string
	ImageURL          string
	Weight            interface{}
	Quantity          int
	UnitPrice         float32
	TaxAmount         float32
	ShippingAmount    float32
	WarehouseLocation string
	Options           interface{}
	ProductID         int
	FulfillmentSKU    string
	Adjustment        bool
	UPC               string
	CreateDate        string
	ModifyDate        string
}

type advancedOptions struct {
	WarehouseID       int
	NonMachinable     bool
	SaturdayDelivery  bool
	ContainsAlcohol   bool
	MergedOrSplit     bool
	MergedIDs         interface{}
	ParentID          interface{}
	StoreID           int
	CustomField1      string
	CustomField2      string
	CustomField3      string
	Source            string
	BillToParty       interface{}
	BillToAccount     interface{}
	BillToPostalCode  interface{}
	BillToCountryCode interface{}
}

type billTo struct {
	Name        string
	Company     string
	Street1     string
	Street2     string
	Street3     string
	City        string
	State       string
	PostalCode  string
	Country     string
	Phone       string
	Residential bool
}

type shipTo struct {
	Name        string
	Company     string
	Street1     string
	Street2     string
	Street3     string
	City        string
	State       string
	PostalCode  string
	Country     string
	Phone       string
	Residential bool
}

type shipStation struct {
	inited bool

	key,
	secret string
}
