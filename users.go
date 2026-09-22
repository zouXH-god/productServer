package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,100}$`)

type userInput struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func validateUserInput(in userInput) error {
	in.Username, in.Email = strings.TrimSpace(in.Username), strings.ToLower(strings.TrimSpace(in.Email))
	if !usernamePattern.MatchString(in.Username) {
		return fmt.Errorf("username must be 3-100 letters, numbers, dots, dashes or underscores")
	}
	if len(in.Password) < 8 || len(in.Password) > 128 {
		return fmt.Errorf("password must be 8-128 characters")
	}
	if in.Email != "" && (!strings.Contains(in.Email, "@") || len(in.Email) > 255) {
		return fmt.Errorf("invalid email")
	}
	return nil
}

func (a *App) registrationEnabled() bool {
	var setting SystemSetting
	return a.db.Where("key=?", "registration_enabled").First(&setting).Error == nil && setting.Value == "true"
}

func (a *App) register(c *gin.Context) {
	if !a.registrationEnabled() {
		fail(c, 403, "registration_disabled", "registration is disabled")
		return
	}
	var in userInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "invalid registration")
		return
	}
	if err := validateUserInput(in); err != nil {
		fail(c, 400, "invalid_user", err.Error())
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	var email *string
	if value := strings.ToLower(strings.TrimSpace(in.Email)); value != "" {
		email = &value
	}
	u := User{Username: strings.TrimSpace(in.Username), Email: email, PasswordHash: string(hash), Enabled: true}
	if err := a.db.Create(&u).Error; err != nil {
		fail(c, 409, "user_exists", "username or email already exists")
		return
	}
	c.JSON(201, u)
}

func (a *App) updateProfile(c *gin.Context) {
	uid := c.MustGet("userID").(uint)
	var in struct {
		Email string `json:"email"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "invalid profile")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email != "" && (!strings.Contains(in.Email, "@") || len(in.Email) > 255) {
		fail(c, 400, "invalid_email", "invalid email")
		return
	}
	var email *string
	if in.Email != "" {
		email = &in.Email
	}
	if err := a.db.Model(&User{}).Where("id=?", uid).Update("email", email).Error; err != nil {
		fail(c, 409, "email_exists", "email already exists")
		return
	}
	a.me(c)
}

func (a *App) changePassword(c *gin.Context) {
	uid := c.MustGet("userID").(uint)
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if c.ShouldBindJSON(&in) != nil || len(in.NewPassword) < 8 || len(in.NewPassword) > 128 {
		fail(c, 400, "invalid_password", "new password must be 8-128 characters")
		return
	}
	var u User
	if a.db.First(&u, uid).Error != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.CurrentPassword)) != nil {
		fail(c, 401, "invalid_credentials", "current password is incorrect")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	a.db.Model(&u).Updates(map[string]any{"password_hash": string(hash), "must_change_password": false, "token_version": gorm.Expr("token_version + 1")})
	c.Status(204)
}

func (a *App) requireAdmin(c *gin.Context) (User, bool) {
	var u User
	if a.db.First(&u, c.MustGet("userID")).Error != nil || !u.Enabled || !u.IsAdmin {
		fail(c, 403, "admin_required", "system administrator required")
		return u, false
	}
	return u, true
}
func (a *App) listUsers(c *gin.Context) {
	if _, ok := a.requireAdmin(c); !ok {
		return
	}
	var users []User
	a.db.Order("id").Find(&users)
	c.JSON(200, users)
}
func (a *App) createUser(c *gin.Context) {
	if _, ok := a.requireAdmin(c); !ok {
		return
	}
	var in userInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "invalid user")
		return
	}
	if err := validateUserInput(in); err != nil {
		fail(c, 400, "invalid_user", err.Error())
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	var email *string
	if value := strings.ToLower(strings.TrimSpace(in.Email)); value != "" {
		email = &value
	}
	u := User{Username: strings.TrimSpace(in.Username), Email: email, PasswordHash: string(hash), Enabled: true, MustChangePassword: true}
	if a.db.Create(&u).Error != nil {
		fail(c, 409, "user_exists", "username or email already exists")
		return
	}
	c.JSON(201, u)
}
func (a *App) updateUser(c *gin.Context) {
	admin, ok := a.requireAdmin(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "userId")
	if !ok {
		return
	}
	var target User
	if a.db.First(&target, id).Error != nil {
		fail(c, 404, "not_found", "user not found")
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
		IsAdmin *bool `json:"is_admin"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "invalid user update")
		return
	}
	if id == admin.ID && in.Enabled != nil && !*in.Enabled {
		fail(c, 400, "self_disable", "cannot disable current user")
		return
	}
	updates := map[string]any{}
	if in.Enabled != nil {
		updates["enabled"] = *in.Enabled
		if !*in.Enabled {
			updates["token_version"] = gorm.Expr("token_version + 1")
		}
	}
	if in.IsAdmin != nil && *in.IsAdmin != target.IsAdmin {
		if !*in.IsAdmin {
			var count int64
			a.db.Model(&User{}).Where("is_admin=? AND enabled=?", true, true).Count(&count)
			if count <= 1 {
				fail(c, 409, "last_admin", "cannot remove the last administrator")
				return
			}
		}
		updates["is_admin"] = *in.IsAdmin
	}
	a.db.Model(&target).Updates(updates)
	a.db.First(&target, id)
	c.JSON(200, target)
}
func (a *App) resetUserPassword(c *gin.Context) {
	if _, ok := a.requireAdmin(c); !ok {
		return
	}
	id, ok := parseID(c, "userId")
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil || len(in.Password) < 8 || len(in.Password) > 128 {
		fail(c, 400, "invalid_password", "password must be 8-128 characters")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	res := a.db.Model(&User{}).Where("id=?", id).Updates(map[string]any{"password_hash": string(hash), "must_change_password": true, "token_version": gorm.Expr("token_version + 1")})
	if res.RowsAffected == 0 {
		fail(c, 404, "not_found", "user not found")
		return
	}
	c.Status(204)
}
func (a *App) getSystemSettings(c *gin.Context) {
	if _, ok := a.requireAdmin(c); !ok {
		return
	}
	c.JSON(200, gin.H{"registration_enabled": a.registrationEnabled()})
}
func (a *App) updateSystemSettings(c *gin.Context) {
	if _, ok := a.requireAdmin(c); !ok {
		return
	}
	var in struct {
		RegistrationEnabled bool `json:"registration_enabled"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "invalid settings")
		return
	}
	value := "false"
	if in.RegistrationEnabled {
		value = "true"
	}
	a.db.Model(&SystemSetting{}).Where("key=?", "registration_enabled").Update("value", value)
	c.JSON(200, gin.H{"registration_enabled": in.RegistrationEnabled})
}
