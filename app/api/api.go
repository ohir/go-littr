package api

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	juju "github.com/juju/errors"
	"github.com/mariusor/littr.go/app"
	ap "github.com/mariusor/littr.go/app/activitypub"
	"github.com/mariusor/littr.go/app/db"
	"github.com/mariusor/littr.go/app/frontend"
	"github.com/mariusor/littr.go/internal/errors"
	"github.com/mariusor/littr.go/internal/log"
	"github.com/spacemonkeygo/httpsig"

	goap "github.com/go-ap/activitypub"
	as "github.com/go-ap/activitystreams"
	j "github.com/go-ap/jsonld"
)

const (
	MaxContentItems = 50
)

type InternalError struct {
}

type UserError struct {
}

type handler struct {
	repo   *repository
	logger log.Logger
	//errorFn app.ErrorHandler
}

type Config struct {
	Logger  log.Logger
	BaseURL string
}

func Init(c Config) handler {
	as.ItemTyperFunc = ap.JSONGetItemByType
	BaseURL = c.BaseURL

	ActorsURL = fmt.Sprintf("%s/self/following", c.BaseURL)
	h := handler{
		logger: c.Logger,
	}
	h.repo = New(c)
	//h.errorFn = h.HandleError
	return h
}

var BaseURL string
var ActorsURL string

const NotFoundStatus = 404
const InternalErrorStatus = 500

type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Fields []Field

type Error struct {
	Code  int
	Error error
}

func Errorf(c int, m string, args ...interface{}) *Error {
	return &Error{c, errors.Errorf(m, args...)}
}

func GetContext() j.Context {
	return j.Context{
		{IRI: j.IRI(as.ActivityBaseURI)},
		{IRI: j.IRI("https://w3id.org/security/v1")},
		{j.Term("score"), j.IRI(fmt.Sprintf("%s/ns#score", app.Instance.BaseURL))},
	}
}

func BuildGlobalOutboxID() as.ObjectID {
	return as.ObjectID(fmt.Sprintf("%s/self/outbox", BaseURL))
}

func BuildActorID(a app.Account) as.ObjectID {
	return as.ObjectID(fmt.Sprintf("%s/%s", ActorsURL, url.PathEscape(a.Hash.String())))
}

func BuildServiceCollectionID(o as.Item) as.ObjectID {
	return as.ObjectID(fmt.Sprintf("%s/%s", BaseURL, getObjectType(o)))
}

func BuildCollectionID(a app.Account, o as.Item) as.ObjectID {
	if len(a.Handle) > 0 {
		return as.ObjectID(fmt.Sprintf("%s/%s/%s", ActorsURL, url.PathEscape(a.Hash.String()), getObjectType(o)))
	}
	return as.ObjectID(fmt.Sprintf("%s/%s", BaseURL, getObjectType(o)))
}

func BuildRepliesCollectionID(i as.Item) as.ObjectID {
	return as.ObjectID(fmt.Sprintf("%s/replies", *i.GetID()))
}

func BuildObjectIDFromItem(i app.Item) (as.ObjectID, bool) {
	if len(i.Hash) == 0 {
		return as.ObjectID(""), false
	}
	if i.SubmittedBy != nil {
		hash := i.SubmittedBy.Hash
		return as.ObjectID(fmt.Sprintf("%s/%s/outbox/%s/object", ActorsURL, url.PathEscape(hash.String()), url.PathEscape(i.Hash.String()))), true
	} else {
		return as.ObjectID(fmt.Sprintf("%s/self/outbox/%s/object", BaseURL, url.PathEscape(i.Hash.String()))), true
	}
}

func BuildObjectIDFromVote(v app.Vote) as.ObjectID {
	att := "liked"
	return as.ObjectID(fmt.Sprintf("%s/%s/%s/%s", ActorsURL, url.PathEscape(v.SubmittedBy.Hash.String()), att, url.PathEscape(v.Item.Hash.String())))
}

func getObjectType(el as.Item) string {
	if el == nil {
		return ""
	}
	var label = ""
	switch el.(type) {
	case *goap.Outbox:
		label = "outbox"
	case goap.Outbox:
		label = "outbox"
	case *goap.Inbox:
		label = "inbox"
	case goap.Inbox:
		label = "inbox"
	case goap.Liked:
		label = "liked"
	case *goap.Liked:
		label = "liked"
	case goap.Followers:
		label = "followers"
	case *goap.Followers:
		label = "followers"
	case goap.Following:
		label = "following"
	case *goap.Following:
		label = "following"
	case as.Person:
		if o, ok := el.(as.Person); ok {
			label = o.Name.First()
		}
	case *as.Person:
		if o, ok := el.(*as.Person); ok {
			label = o.Name.First()
		}
	case ap.Person:
		if o, ok := el.(ap.Person); ok {
			label = o.Name.First()
		}
	case *ap.Person:
		if o, ok := el.(*ap.Person); ok {
			label = o.Name.First()
		}
	}
	return label
}

func httpErrorResponse(e error) int {
	if errors.IsBadRequest(e) {
		return http.StatusBadRequest
	}
	if errors.IsForbidden(e) {
		return http.StatusForbidden
	}
	if errors.IsNotSupported(e) {
		return http.StatusHTTPVersionNotSupported
	}
	if errors.IsMethodNotAllowed(e) {
		return http.StatusMethodNotAllowed
	}
	if errors.IsNotFound(e) {
		return http.StatusNotFound
	}
	if errors.IsNotImplemented(e) {
		return http.StatusNotImplemented
	}
	if errors.IsUnauthorized(e) {
		return http.StatusUnauthorized
	}
	if errors.IsTimeout(e) {
		return http.StatusGatewayTimeout
	}
	if errors.IsNotValid(e) {
		return http.StatusNotAcceptable
	}
	if errors.IsMethodNotAllowed(e) {
		return http.StatusMethodNotAllowed
	}
	return http.StatusInternalServerError
}

type e struct {
	Code     int      `json:"code,omitempty"`
	Message  string   `json:"message"`
	Trace    []string `json:"trace,omitempty"`
	Location string   `json:"location,omitempty"`
}

func httpError(err error) e {
	var msg string
	var loc string
	var trace []string

	if errors.IsBadRequest(err) {
		err = juju.Cause(err)
	}
	if errors.IsForbidden(err) {
		err = juju.Cause(err)
	}
	if errors.IsNotSupported(err) {
		err = juju.Cause(err)
	}
	if errors.IsMethodNotAllowed(err) {
		err = juju.Cause(err)
	}
	if errors.IsNotFound(err) {
		err = juju.Cause(err)
	}
	if errors.IsNotImplemented(err) {
		err = juju.Cause(err)
	}
	if errors.IsUnauthorized(err) {
		err = juju.Cause(err)
	}
	if errors.IsTimeout(err) {
		err = juju.Cause(err)
	}
	if errors.IsNotValid(err) {
		err = juju.Cause(err)
	}
	if errors.IsMethodNotAllowed(err) {
		err = juju.Cause(err)
	}
	switch e := juju.Cause(err).(type) {
	case *json.UnmarshalTypeError:
		msg = fmt.Sprintf("%T: Value[%s] Type[%v]\n", e, e.Value, e.Type)
	case *json.InvalidUnmarshalError:
		msg = fmt.Sprintf("%T: Type[%v]\n", e, e.Type)
	case *juju.Err:
		msg = fmt.Sprintf("%s", e.Error())
		if app.Instance.Config.Env == app.DEV {
			trace = e.StackTrace()
			f, l := e.Location()
			if len(f) > 0 {
				loc = fmt.Sprintf("%s:%d", f, l)
			}
		}
	default:
		msg = e.Error()
	}
	return e{
		Message:  msg,
		Trace:    trace,
		Location: loc,
		Code:     httpErrorResponse(err),
	}
}

func (h handler) HandleError(w http.ResponseWriter, r *http.Request, errs ...error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	type eresp struct {
		Status int `json:"status,omitempty"`
		Errors []e `json:"errors"`
	}
	res := eresp{
		Errors: []e{},
	}

	for _, err := range errs {
		if err == nil {
			continue
		}
		e := httpError(err)
		if res.Status < e.Code {
			res.Status = e.Code
		}
		e.Code = 0
		res.Errors = append(res.Errors, e)
	}
	if res.Status == 0 {
		res.Status = http.StatusInternalServerError
	}

	j, _ := json.Marshal(res)
	w.WriteHeader(res.Status)
	w.Write(j)
}

type keyLoader struct {
	acc   app.Account
	LogFn func(string, ...interface{})
}

func (k keyLoader) log(s string, p ...interface{}) {
	if k.LogFn != nil {
		k.LogFn(s, p...)
	}
}

func loadFederatedActor(id as.IRI) (as.Actor, error) {
	return as.Object{}, errors.NotImplementedf("federated actors loading is not implemented")
}

func (k *keyLoader) GetKey(id string) interface{} {
	var err error

	u, err := url.Parse(id)
	if err != nil {
		return err
	}
	if u.Fragment != "main-key" {
		// invalid generated public key id
		k.log("missing key")
		return nil
	}

	if err := validateLocalIRI(as.IRI(id)); err == nil {
		hash := path.Base(u.Path)
		k.acc, err = db.Config.LoadAccount(app.LoadAccountsFilter{Key: app.Hashes{app.Hash(hash)}})
		if err != nil {
			k.log("unable to find local account matching key id %s", id)
			return nil
		}
	} else {
		// @todo(queue_support): this needs to be moved to using queues
		actor, err := loadFederatedActor(as.IRI(u.RequestURI()))
		if err != nil {
			k.log( "unable to load federated account matching key id %s", id)
			return nil
		}
		if err := k.acc.FromActivityPub(actor); err != nil {
			k.log("failed to load account: %s", err)
			return nil
		}
	}

	var pub crypto.PublicKey
	pub, err = x509.ParsePKIXPublicKey(k.acc.Metadata.Key.Public)
	if err != nil {
		k.log("x509 error %s", err)
		return nil
	}
	return pub
}

func httpSignatureVerifier(getter *keyLoader) (*httpsig.Verifier, string) {
	realm := app.Instance.HostName
	v := httpsig.NewVerifier(getter)
	v.SetRequiredHeaders([]string{"(request-target)", "host", "date"})

	var challengeParams []string
	if realm != "" {
		challengeParams = append(challengeParams, fmt.Sprintf("realm=%q", realm))
	}
	if headers := v.RequiredHeaders(); len(headers) > 0 {
		challengeParams = append(challengeParams, fmt.Sprintf("headers=%q", strings.Join(headers, " ")))
	}

	challenge := "Signature"
	if len(challengeParams) > 0 {
		challenge += fmt.Sprintf(" %s", strings.Join(challengeParams, ", "))
	}
	return v, challenge
}

func (h handler) loadAccountFromHttpSig(w http.ResponseWriter, r *http.Request) (app.Account, error) {
	getter := keyLoader{ acc: app.AnonymousAccount }
	getter.LogFn = h.logger.WithContext(log.Ctx{"from": "getter"}).Debugf

	v, challenge := httpSignatureVerifier(&getter)
	var acct = frontend.AnonymousAccount()
	if r.Header["Authorization"] != nil {
		// only verify http-signature if present
		if err := v.Verify(r); err != nil {
			w.Header().Add("WWW-Authenticate", challenge)
			h.logger.WithContext(log.Ctx{
				"handle": acct.Handle,
				"hash":   acct.Hash,
				"auth":   r.Header.Get("Authorization"),
				"req":    fmt.Sprintf("%s:%s", r.Method, r.URL.RequestURI()),
				"err":    err,
			}).Warn("invalid HTTP signature")
			// TODO(marius): here we need to implement some outside logic, as to we want to allow non-signed
			//   requests on some urls, but not on others - probably another handler to check for Anonymous
			//   would suffice.
			return getter.acc, err
		} else {
			acct = getter.acc
			h.logger.WithContext(log.Ctx{
				"handle": acct.Handle,
				"hash":   acct.Hash,
			}).Debug("loaded account from HTTP-sig header")
		}
	}
	return getter.acc, nil
}

type verifierFn func(a app.Account) error

func None(a app.Account) error {
	return nil
}

func NotAnonymous(a app.Account) error {
	if a.Hash == app.AnonymousHash && a.IsLocal() {
		return errors.Forbiddenf("forbidden for %s actor", a.Handle)
	}
	return nil
}

func LocalAccount(a app.Account) error {
	if !a.IsLocal() {
		return errors.NotFoundf("%s is not a local actor", a.Handle)
	}
	return nil
}

func (h handler) VerifyHttpSignature(fns ...verifierFn) app.Handler {
	return func(next http.Handler) http.Handler {
		fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			acct, e := h.loadAccountFromHttpSig(w, r)
			if e != nil {
				h.HandleError(w, r, errors.NewForbidden(e, fmt.Sprintf("forbidden for %s actor", acct.Handle)))
				return
			}
			for _, f := range fns {
				if err := f(acct); err != nil {
					h.HandleError(w, r, err)
					return
				}
			}
			ctx := context.WithValue(r.Context(), app.AccountCtxtKey, acct)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
		return http.HandlerFunc(fn)
	}
}
