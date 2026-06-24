package branches

// Branch is a clinic location (FM_ORG_ID) for reminder and dashboard display.
type Branch struct {
	Name    string
	Address string
}

// ByID maps FM_ORG_ID → name and address (three active clinic branches).
var ByID = map[int]Branch{
	3:   {Name: "Куркино", Address: "г. Москва, ул. Ландышевая, 14к1"},
	496: {Name: "Куркино 2", Address: "г. Москва, ул. Воротынская, 4"},
	106: {Name: "Каширка", Address: "г. Москва, Каширское шоссе, 74к1"},
}

// ShortLabel returns branch name for compact reminder text (PR #1).
func ShortLabel(branchID int, branchCode string) string {
	if b, ok := ByID[branchID]; ok {
		return b.Name
	}
	return branchCode
}
