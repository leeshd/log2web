package main

// Herric UUID Seed,
import (
	"git.ceyes.cn/cloud/pkg/log"
	"github.com/nu7hatch/gouuid"
)

// IUIdGen Interface
type IUIdGen interface {
	GenUId() string              // Return UId such as "h_3Wir92" instead of "dd415c7e-1b53-4e9a-46f2-3abb7c894a62"
	IsUniqueUId(uid string) bool // Need to implement by implement instance
}

// CeUIdGen
// Performance: X220, Windows 8.1 64bit, Generate 10,000,000 UId in 29.3s
type CeUIdGen struct {
	chUId    chan string // UId channel
	chUIdInd chan uint   // Ind channel for UId channel
}

func NewCeUIdGen() *CeUIdGen {
	ig := &CeUIdGen{}

	ig.chUId = make(chan string, 16)
	ig.chUIdInd = make(chan uint)

	// Prepare UId in channel
	go ig.start()

	return ig
}

func (ig *CeUIdGen) GenUId() string {
	return <-ig.chUId
}

func (ig *CeUIdGen) IsUniqueUId(uid string) bool {
	// I believe the possibility of duplicate is tiny
	return true
}

func (ig *CeUIdGen) start() {
	m := []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_-")

Loop:
	for {
		// Generate new Guid
		u4, err := uuid.NewV4()
		if err != nil {
			log.Ef("error:", err)

			continue
		}

		// Combine 2 bytes -> m[x], for 8 times
		uid := ""
		for i := 0; i < 16; i += 2 {
			u := u4[i]<<4 + u4[i+1]
			j := u % 64
			uid += string(m[j])
		}
		// If not unique, retry
		if !ig.IsUniqueUId(uid) {
			continue
		}
		select {
		case ig.chUId <- uid:
		case _ = <-ig.chUIdInd:
			log.D("close chUId")
			close(ig.chUId)
			break Loop
		}
	}
}

func (ig *CeUIdGen) Stop() {
	// Send indication to close chUId channel
	ig.chUIdInd <- 0
}
