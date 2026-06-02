package domain

type User struct {
	UserID       uint64 `json:"userId,string" protobuf:"fixed64,1,opt,name=userId"`
	Name         string `json:"name" protobuf:"bytes,2,opt,name=name"`
	Username     string `json:"username" protobuf:"bytes,3,opt,name=username"`
	PasswordHash string `json:"-" protobuf:"bytes,4,opt,name=passwordHash"`
}

type Category struct {
	CategoryID uint64  `json:"categoryId,string" protobuf:"fixed64,1,opt,name=categoryId"`
	Name       string  `json:"name" protobuf:"bytes,2,opt,name=name"`
	ParentID   *uint64 `json:"parentId,omitempty,string" protobuf:"fixed64,3,opt,name=parentId"`
	SortOrder  int32   `json:"sortOrder" protobuf:"varint,4,opt,name=sortOrder"`
}

type Merchant struct {
	MerchantID uint64 `json:"merchantId,string" protobuf:"fixed64,1,opt,name=merchantId"`
	Name       string `json:"name" protobuf:"bytes,2,opt,name=name"`
}

type Item struct {
	ItemID     uint64   `json:"itemId,string" protobuf:"fixed64,1,opt,name=itemId"`
	Name       string   `json:"name" protobuf:"bytes,2,opt,name=name"`
	CategoryID uint64   `json:"categoryId,string" protobuf:"fixed64,3,opt,name=categoryId"`
	MerchantID uint64   `json:"merchantId,string" protobuf:"fixed64,4,opt,name=merchantId"`
	Normalized string   `json:"normalized" protobuf:"bytes,5,opt,name=normalized"`
	Aliases    []string `json:"aliases,omitempty" protobuf:"bytes,6,rep,name=aliases"`
}

type Receipt struct {
	ReceiptID    uint64        `json:"receiptId,string" protobuf:"fixed64,1,opt,name=receiptId"`
	MerchantID   uint64        `json:"merchantId,string" protobuf:"fixed64,2,opt,name=merchantId"`
	OwnerID      uint64        `json:"ownerId,string" protobuf:"fixed64,3,opt,name=ownerId"`
	Date         int64         `json:"date" protobuf:"fixed64,4,opt,name=date"`
	PhotoURL     string        `json:"photoUrl,omitempty" protobuf:"bytes,5,opt,name=photoUrl"`
	Items        []ReceiptItem `json:"items" protobuf:"bytes,6,rep,name=items"`
	TotalCents   int64         `json:"totalCents" protobuf:"fixed64,7,opt,name=totalCents"`
}

type ReceiptItem struct {
	ItemID         uint64  `json:"itemId,string" protobuf:"fixed64,1,opt,name=itemId"`
	Quantity       float64 `json:"quantity" protobuf:"fixed64,2,opt,name=quantity"`
	UnitPriceCents int64  `json:"unitPriceCents" protobuf:"fixed64,3,opt,name=unitPriceCents"`
}

type Proposal struct {
	ProposalID    uint64         `json:"proposalId,string" protobuf:"fixed64,1,opt,name=proposalId"`
	OwnerID       uint64         `json:"ownerId,string" protobuf:"fixed64,2,opt,name=ownerId"`
	MerchantID    uint64         `json:"merchantId,string" protobuf:"fixed64,3,opt,name=merchantId"`
	Merchant      string         `json:"merchant" protobuf:"bytes,4,opt,name=merchant"`
	Date          int64          `json:"date" protobuf:"fixed64,5,opt,name=date"`
	PhotoURL      string         `json:"photoUrl,omitempty" protobuf:"bytes,6,opt,name=photoUrl"`
	Items         []ProposalItem `json:"items" protobuf:"bytes,7,rep,name=items"`
	TotalCents    int64          `json:"totalCents" protobuf:"fixed64,8,opt,name=totalCents"`
	Status        string         `json:"status" protobuf:"bytes,9,opt,name=status"`
	Error         string         `json:"error,omitempty" protobuf:"bytes,10,opt,name=error"`
	OriginalHash  string         `json:"originalHash,omitempty" protobuf:"bytes,11,opt,name=originalHash"`
}

type ProposalItem struct {
	ParsedName       string  `json:"parsedName" protobuf:"bytes,1,opt,name=parsedName"`
	Quantity         float64 `json:"quantity" protobuf:"fixed64,2,opt,name=quantity"`
	UnitPriceCents   int64   `json:"unitPriceCents" protobuf:"fixed64,3,opt,name=unitPriceCents"`
	MatchedItemID    uint64  `json:"matchedItemId,omitempty,string" protobuf:"fixed64,4,opt,name=matchedItemId"`
	CategoryID       uint64  `json:"categoryId,omitempty,string" protobuf:"fixed64,5,opt,name=categoryId"`
	IsNewCategory    bool    `json:"isNewCategory,omitempty" protobuf:"varint,6,opt,name=isNewCategory"`
	UserChoice       string  `json:"userChoice,omitempty" protobuf:"bytes,7,opt,name=userChoice"`
}
