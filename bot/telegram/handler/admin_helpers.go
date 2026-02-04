package handler

func isBotAdmin(adminIDs map[int64]struct{}, userID int64) bool {
	if len(adminIDs) == 0 {
		return false
	}
	_, ok := adminIDs[userID]
	return ok
}
