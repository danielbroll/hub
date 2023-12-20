package main

import (
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	echologrus "github.com/davrux/echo-logrus/v4"
	// "github.com/getAlby/lndhub.go/lib/responses"
	"github.com/getAlby/nostr-wallet-connect/frontend"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/nbd-wtf/go-nostr"
	"github.com/sirupsen/logrus"
	ddEcho "gopkg.in/DataDog/dd-trace-go.v1/contrib/labstack/echo.v4"
	"gorm.io/gorm"
)

//go:embed public/*
var embeddedAssets embed.FS

//go:embed views/*
var embeddedViews embed.FS

type TemplateRegistry struct {
	templates map[string]*template.Template
}

// Implement e.Renderer interface
func (t *TemplateRegistry) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	tmpl, ok := t.templates[name]
	if !ok {
		err := errors.New("Template not found -> " + name)
		return err
	}
	return tmpl.ExecuteTemplate(w, "layout.html", data)
}

func (svc *Service) RegisterSharedRoutes(e *echo.Echo) {
	e.HideBanner = true
	e.Use(echologrus.Middleware())
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "form:_csrf",
	}))
	e.Use(session.Middleware(sessions.NewCookieStore([]byte(svc.cfg.CookieSecret))))
	e.Use(ddEcho.Middleware(ddEcho.WithServiceName("nostr-wallet-connect")))

	assetSubdir, _ := fs.Sub(embeddedAssets, "public")
	assetHandler := http.FileServer(http.FS(assetSubdir))
	e.GET("/public/*", echo.WrapHandler(http.StripPrefix("/public/", assetHandler)))
	e.GET("/api/getCSRFToken", svc.CSRFHandler)
	e.GET("/api/apps", svc.AppsListHandler)
	e.GET("/api/apps/:pubkey", svc.AppsShowHandler)
	e.POST("/api/apps", svc.AppsCreateHandler)
	e.POST("/api/apps/delete/:pubkey", svc.AppsDeleteHandler)
	e.GET("/api/info", svc.InfoHandler)
	e.GET("/logout", svc.LogoutHandler)
	e.GET("/", svc.IndexHandler)
	frontend.RegisterHandlers(e)
}

func (svc *Service) IndexHandler(c echo.Context) error {
	sess, _ := session.Get(CookieName, c)
	returnTo := sess.Values["return_to"]
	user, err := svc.GetUser(c)
	if err != nil {
		return err
	}
	if user != nil && returnTo != nil {
		delete(sess.Values, "return_to")
		sess.Options.MaxAge = 0
		sess.Options.SameSite = http.SameSiteLaxMode
		if svc.cfg.CookieDomain != "" {
			sess.Options.Domain = svc.cfg.CookieDomain
		}
		sess.Save(c.Request(), c.Response())
		return c.Redirect(302, fmt.Sprintf("%s", returnTo))
	}
	if user != nil {
		return c.Redirect(302, "/apps")
	}
	return c.Redirect(302, "/login")
}

func (svc *Service) AboutHandler(c echo.Context) error {
	user, err := svc.GetUser(c)
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "about.html", map[string]interface{}{
		"User": user,
	})
}

func (svc *Service) AppsListHandler(c echo.Context) error {
	user, err := svc.GetUser(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   true,
			Code:    8,
			Message: fmt.Sprintf("Bad arguments %s", err.Error()),
		})
	}
	if user == nil {
		// TODO: Show not found?
		return c.Redirect(302, "/?q=notfound")
	}

	apps := user.Apps

	lastEvents := make(map[uint]NostrEvent)
	eventsCounts := make(map[uint]int64)
	for _, app := range apps {
		var lastEvent NostrEvent
		var eventsCount int64
		svc.db.Where("app_id = ?", app.ID).Order("id desc").Limit(1).Find(&lastEvent)
		svc.db.Model(&NostrEvent{}).Where("app_id = ?", app.ID).Count(&eventsCount)
		lastEvents[app.ID] = lastEvent
		eventsCounts[app.ID] = eventsCount
	}

	return c.JSON(http.StatusOK, ListAppsResponse{
		Apps:         apps,
		LastEvents:   lastEvents,
		EventsCounts: eventsCounts,
	})
}

func (svc *Service) AppsShowHandler(c echo.Context) error {
	csrf, _ := c.Get(middleware.DefaultCSRFConfig.ContextKey).(string)
	user, err := svc.GetUser(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   true,
			Code:    8,
			Message: fmt.Sprintf("Bad arguments %s", err.Error()),
		})
	}
	if user == nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   true,
			Code:    8,
			Message: "User does not exist",
		})
	}

	app := App{}
	svc.db.Where("user_id = ? AND nostr_pubkey = ?", user.ID, c.Param("pubkey")).First(&app)

	if app.NostrPubkey == "" {
		// TODO: Show not found?
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   true,
			Code:    8,
			Message: "App does not exist",
		})
	}

	lastEvent := NostrEvent{}
	svc.db.Where("app_id = ?", app.ID).Order("id desc").Limit(1).Find(&lastEvent)
	var eventsCount int64
	svc.db.Model(&NostrEvent{}).Where("app_id = ?", app.ID).Count(&eventsCount)

	paySpecificPermission := AppPermission{}
	appPermissions := []AppPermission{}
	expiresAt := time.Time{}
	svc.db.Where("app_id = ?", app.ID).Find(&appPermissions)

	requestMethods := []string{}
	for _, appPerm := range appPermissions {
		if expiresAt.IsZero() && !appPerm.ExpiresAt.IsZero() {
			expiresAt = appPerm.ExpiresAt
		}
		if appPerm.RequestMethod == NIP_47_PAY_INVOICE_METHOD {
			//find the pay_invoice-specific permissions
			paySpecificPermission = appPerm
		}
		requestMethods = append(requestMethods, appPerm.RequestMethod)
	}

	expiresAtFormatted := expiresAt.Format("January 2, 2006 03:04 PM")

	renewsIn := ""
	budgetUsage := int64(0)
	maxAmount := paySpecificPermission.MaxAmount
	if maxAmount > 0 {
		budgetUsage = svc.GetBudgetUsage(&paySpecificPermission)
		endOfBudget := GetEndOfBudget(paySpecificPermission.BudgetRenewal, app.CreatedAt)
		renewsIn = getEndOfBudgetString(endOfBudget)
	}

	return c.JSON(http.StatusOK, ShowAppResponse{
		App:                   app,
		PaySpecificPermission: paySpecificPermission,
		RequestMethods:        requestMethods,
		ExpiresAt:             expiresAt.Unix(),
		ExpiresAtFormatted:    expiresAtFormatted,
		LastEvent:             lastEvent,
		EventsCount:           eventsCount,
		BudgetUsage:           budgetUsage,
		RenewsIn:              renewsIn,
		Csrf:                  csrf,
	})
}

func getEndOfBudgetString(endOfBudget time.Time) (result string) {
	if endOfBudget.IsZero() {
		return "--"
	}
	endOfBudgetDuration := endOfBudget.Sub(time.Now())

	//less than a day
	if endOfBudgetDuration.Hours() < 24 {
		hours := int(endOfBudgetDuration.Hours())
		minutes := int(endOfBudgetDuration.Minutes()) % 60
		return fmt.Sprintf("%d hours and %d minutes", hours, minutes)
	}
	//less than a month
	if endOfBudgetDuration.Hours() < 24*30 {
		days := int(endOfBudgetDuration.Hours() / 24)
		return fmt.Sprintf("%d days", days)
	}
	//more than a month
	months := int(endOfBudgetDuration.Hours() / 24 / 30)
	days := int(endOfBudgetDuration.Hours()/24) % 30
	if days > 0 {
		return fmt.Sprintf("%d months %d days", months, days)
	}
	return fmt.Sprintf("%d months", months)
}

func (svc *Service) CSRFHandler(c echo.Context) error {
	csrf, _ := c.Get(middleware.DefaultCSRFConfig.ContextKey).(string)
	return c.JSON(http.StatusOK, &CSRFResponse{
		Csrf: csrf,
	})
}

func (svc *Service) AppsCreateHandler(c echo.Context) error {
	user, err := svc.GetUser(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   true,
			Code:    8,
			Message: fmt.Sprintf("Bad arguments %s", err.Error()),
		})
	}
	if user == nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   true,
			Code:    8,
			Message: "User does not exist",
		})
	}

	name := c.FormValue("name")
	var pairingPublicKey string
	var pairingSecretKey string
	if c.FormValue("pubkey") == "" {
		pairingSecretKey = nostr.GeneratePrivateKey()
		pairingPublicKey, _ = nostr.GetPublicKey(pairingSecretKey)
	} else {
		pairingPublicKey = c.FormValue("pubkey")
		//validate public key
		decoded, err := hex.DecodeString(pairingPublicKey)
		if err != nil || len(decoded) != 32 {
			svc.Logger.Errorf("Invalid public key format: %s", pairingPublicKey)
			return c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   true,
				Code:    8,
				Message: fmt.Sprintf("Invalid public key format: %s", pairingPublicKey),
			})
		}
	}
	app := App{Name: name, NostrPubkey: pairingPublicKey}
	maxAmount, _ := strconv.Atoi(c.FormValue("maxAmount"))
	budgetRenewal := c.FormValue("budgetRenewal")

	expiresAt := time.Time{}
	if c.FormValue("expiresAt") != "" {
		expiresAt, err = time.Parse(time.RFC3339, c.FormValue("expiresAt"))
		if err != nil {
			svc.Logger.Errorf("Invalid expiresAt: %s", pairingPublicKey)
			return c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   true,
				Code:    8,
				Message: fmt.Sprintf("Invalid expiresAt: %v", err),
			})
		}
	}

	if !expiresAt.IsZero() {
		expiresAt = time.Date(expiresAt.Year(), expiresAt.Month(), expiresAt.Day(), 23, 59, 59, 0, expiresAt.Location())
	}

	err = svc.db.Transaction(func(tx *gorm.DB) error {
		err = tx.Model(&user).Association("Apps").Append(&app)
		if err != nil {
			return err
		}

		requestMethods := c.FormValue("requestMethods")
		if requestMethods == "" {
			return fmt.Errorf("Won't create an app without request methods.")
		}
		//request methods should be space separated list of known request kinds
		methodsToCreate := strings.Split(requestMethods, " ")
		for _, m := range methodsToCreate {
			//if we don't know this method, we return an error
			if _, ok := nip47MethodDescriptions[m]; !ok {
				return fmt.Errorf("Did not recognize request method: %s", m)
			}
			appPermission := AppPermission{
				App:           app,
				RequestMethod: m,
				ExpiresAt:     expiresAt,
				//these fields are only relevant for pay_invoice
				MaxAmount:     maxAmount,
				BudgetRenewal: budgetRenewal,
			}
			err = tx.Create(&appPermission).Error
			if err != nil {
				return err
			}
		}
		// commit transaction
		return nil
	})

	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"pairingPublicKey": pairingPublicKey,
			"name":             name,
		}).Errorf("Failed to save app: %v", err)
		return c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   true,
			Code:    8,
			Message: fmt.Sprintf("Failed to save app: %v", err),
		})
	}

	publicRelayUrl := svc.cfg.PublicRelay
	if publicRelayUrl == "" {
		publicRelayUrl = svc.cfg.Relay
	}

	responseBody := &CreateAppResponse{}
	responseBody.Name = name
	responseBody.Pubkey = pairingPublicKey
	responseBody.PairingSecret = pairingSecretKey

	if c.FormValue("returnTo") != "" {
		returnToUrl, err := url.Parse(c.FormValue("returnTo"))
		if err == nil {
			query := returnToUrl.Query()
			query.Add("relay", publicRelayUrl)
			query.Add("pubkey", svc.cfg.IdentityPubkey)
			if user.LightningAddress != "" {
				query.Add("lud16", user.LightningAddress)
			}
			returnToUrl.RawQuery = query.Encode()
			responseBody.ReturnTo = returnToUrl.String()
		}
	}

	var lud16 string
	if user.LightningAddress != "" {
		lud16 = fmt.Sprintf("&lud16=%s", user.LightningAddress)
	}
	responseBody.PairingUri = fmt.Sprintf("nostr+walletconnect://%s?relay=%s&secret=%s%s", svc.cfg.IdentityPubkey, publicRelayUrl, pairingSecretKey, lud16)
	return c.JSON(http.StatusOK, responseBody)
}

func (svc *Service) AppsDeleteHandler(c echo.Context) error {
	user, err := svc.GetUser(c)
	if err != nil {
		return err
	}
	if user == nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   true,
			Code:    8,
			Message: "User does not exist",
		})
	}
	app := App{}
	svc.db.Where("user_id = ? AND nostr_pubkey = ?", user.ID, c.Param("pubkey")).First(&app)
	svc.db.Delete(&app)
	return c.JSON(http.StatusOK, "App deleted successfully")
}

func (svc *Service) LogoutHandler(c echo.Context) error {
	sess, _ := session.Get(CookieName, c)
	sess.Options.MaxAge = -1
	if svc.cfg.CookieDomain != "" {
		sess.Options.Domain = svc.cfg.CookieDomain
	}
	sess.Save(c.Request(), c.Response())
	return c.JSON(http.StatusOK, "Logout successful")
}

func (svc *Service) InfoHandler(c echo.Context) error {
	csrf, _ := c.Get(middleware.DefaultCSRFConfig.ContextKey).(string)
	user, err := svc.GetUser(c)
	if err != nil {
		return err
	}
	responseBody := &InfoResponse{}
	responseBody.BackendType = svc.cfg.LNBackendType
	responseBody.User = *user
	responseBody.Csrf = csrf
	return c.JSON(http.StatusOK, responseBody)
}
