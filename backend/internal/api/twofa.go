package api

import (
	"net/http"

	"gerrit-go/internal/auth"
	"gerrit-go/internal/i18n"
)

const totpIssuer = "Gerrit Go"

// handleGet2FA reports the caller's current 2FA state.
func (s *Server) handleGet2FA(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	secret, enabled, _ := s.db.GetTOTP(acct.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":  enabled,
		"enrolled": secret != "",
	})
}

// handleEnroll2FA generates a fresh pending secret and returns it with its
// otpauth:// provisioning URI. It does not enable 2FA until confirmed.
func (s *Server) handleEnroll2FA(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if _, enabled, _ := s.db.GetTOTP(acct.ID); enabled {
		writeErr(w, http.StatusConflict, "2FA is already enabled; disable it first")
		return
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}
	if err := s.db.SetTOTPSecret(acct.ID, secret); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"secret":      secret,
		"otpauth_uri": auth.OTPAuthURI(totpIssuer, acct.Username, secret),
	})
}

// handleEnable2FA confirms a pending enrollment by validating a code, then
// starts enforcing 2FA at login.
func (s *Server) handleEnable2FA(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFrom(r.Context())
	acct := s.account(r)
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, i18n.T(lang, "err.invalidBody"))
		return
	}
	secret, enabled, _ := s.db.GetTOTP(acct.ID)
	if enabled {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if secret == "" {
		writeErr(w, http.StatusBadRequest, "enroll first")
		return
	}
	if !auth.ValidateTOTP(secret, req.Code) {
		writeErr(w, http.StatusBadRequest, i18n.T(lang, "err.totpInvalid"))
		return
	}
	if err := s.db.SetTOTPEnabled(acct.ID, true); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to enable 2FA")
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// handleDisable2FA turns off 2FA after validating a current code, and clears
// the stored secret.
func (s *Server) handleDisable2FA(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFrom(r.Context())
	acct := s.account(r)
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, i18n.T(lang, "err.invalidBody"))
		return
	}
	secret, enabled, _ := s.db.GetTOTP(acct.ID)
	if enabled && !auth.ValidateTOTP(secret, req.Code) {
		writeErr(w, http.StatusForbidden, i18n.T(lang, "err.totpInvalid"))
		return
	}
	if err := s.db.SetTOTPEnabled(acct.ID, false); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to disable 2FA")
		return
	}
	s.db.SetTOTPSecret(acct.ID, "")
	writeJSON(w, http.StatusOK, nil)
}
