package api

import (
	"encoding/json"
	"fmt"
	"github.com/mariusor/littr.go/app"
	"github.com/mariusor/littr.go/app/db"
	"github.com/mariusor/littr.go/app/models"
	"math"
	"net/http"
)

var Desc = app.Desc{
	Title:       "litter dot me",
	Description: "Littr.me is a link aggregator similar to reddit or hacker news",
	Email:       "system@littr.me",
	Lang:        []string{"en"},
}

// GET /api/v1/instance
// In order to be compatible with Mastodon
func ShowInstance(w http.ResponseWriter, r *http.Request) {
	ifErr := func(err ...error) {
		if err != nil && len(err) > 0 && err[0] != nil {
			HandleError(w, r, http.StatusInternalServerError, err...)
			return
		}
	}

	u, err := db.Config.LoadAccounts(models.LoadAccountsFilter{
		MaxItems: math.MaxInt64,
	})
	ifErr(err)
	i, err := db.Config.LoadItems(models.LoadItemsFilter{
		MaxItems: math.MaxInt64,
	})
	ifErr(err)

	Desc.Stats.DomainCount = 1
	Desc.Stats.UserCount = len(u)
	Desc.Stats.StatusCount = len(i)
	Desc.Uri = app.Instance.HostName
	Desc.Version = fmt.Sprintf("2.5.0 compatible (littr.me %s)", app.Instance.Version)

	data, err := json.Marshal(Desc)
	ifErr(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// GET /api/v1/instance/peers
// In order to be compatible with Mastodon
func ShowPeers(w http.ResponseWriter, r *http.Request) {
	em := []string{app.Instance.HostName}
	data, _ := json.Marshal(em)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

type activity struct {
	Week         int `json:"week"`
	Statuses     int `json:"statuses"`
	Logins       int `json:"logins"`
	Registration int `json:"registrations"`
}

// GET /api/v1/instance/activity
// In order to be compatible with Mastodon
func ShowActivity(w http.ResponseWriter, r *http.Request) {
	em := []activity{}
	data, _ := json.Marshal(em)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}