package transfer

// killLinks severs the sender side of every live link, as a network drop would.
func killLinks() {
	if r := lastRoom.Load(); r != nil {
		r.dropAll()
	}
}
