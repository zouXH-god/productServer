package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var environmentName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

type environmentPayload struct {
	Name, Value string
	Sensitive   bool
}
type environmentSnapshot struct {
	Global, Project map[string]string
	Secrets         []string
}

func redactEnvironment(v EnvironmentVariable) EnvironmentVariable {
	if v.Sensitive {
		v.Value = ""
	}
	v.ValueEncrypted = ""
	return v
}
func (a *App) listEnvironment(c *gin.Context, ownerID, projectID uint) {
	var values []EnvironmentVariable
	a.db.Where("user_id=? AND project_id=?", ownerID, projectID).Order("name").Find(&values)
	for i := range values {
		values[i] = redactEnvironment(values[i])
	}
	c.JSON(200, values)
}
func (a *App) listGlobalEnvironmentVariables(c *gin.Context) {
	a.listEnvironment(c, c.MustGet("userID").(uint), 0)
}
func (a *App) listProjectEnvironmentVariables(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectView)
	if !ok {
		return
	}
	a.listEnvironment(c, project.UserID, id)
}

func (a *App) listAvailableEnvironmentVariables(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectView)
	if !ok {
		return
	}
	var values []EnvironmentVariable
	if err := a.db.Where("user_id=? AND project_id IN ?", project.UserID, []uint{0, id}).Order("name, project_id").Find(&values).Error; err != nil {
		fail(c, 500, "environment_variables_unavailable", "unable to load environment variables")
		return
	}
	projectNames := map[string]bool{}
	for _, value := range values {
		if value.ProjectID == id {
			projectNames[value.Name] = true
		}
	}
	type availableVariable struct {
		ID         uint   `json:"id"`
		Name       string `json:"name"`
		Value      string `json:"value,omitempty"`
		Sensitive  bool   `json:"sensitive"`
		Scope      string `json:"scope"`
		Overridden bool   `json:"overridden"`
	}
	result := make([]availableVariable, 0, len(values))
	for _, value := range values {
		scope := "global"
		if value.ProjectID == id {
			scope = "project"
		}
		plain := value.Value
		if value.Sensitive {
			plain = ""
		}
		result = append(result, availableVariable{ID: value.ID, Name: value.Name, Value: plain, Sensitive: value.Sensitive, Scope: scope, Overridden: scope == "global" && projectNames[value.Name]})
	}
	c.JSON(200, result)
}
func (a *App) saveEnvironment(c *gin.Context, ownerID, projectID uint, idParam string) {
	var in environmentPayload
	if c.ShouldBindJSON(&in) != nil || !environmentName.MatchString(in.Name) {
		fail(c, 400, "invalid_environment_variable", "name must match [A-Z_][A-Z0-9_]*")
		return
	}
	uid := ownerID
	var value EnvironmentVariable
	creating := idParam == ""
	if !creating {
		id, _ := strconv.Atoi(idParam)
		if a.db.Where("id=? AND user_id=? AND project_id=?", id, uid, projectID).First(&value).Error != nil {
			fail(c, 404, "not_found", "environment variable not found")
			return
		}
	}
	value.UserID, value.ProjectID, value.Name, value.Sensitive = uid, projectID, in.Name, in.Sensitive
	if in.Sensitive {
		if in.Value != "" {
			encrypted, err := encryptSecret(a.cfg.SecretEncryptionKey, in.Value)
			if err != nil {
				fail(c, 400, "encryption_unavailable", err.Error())
				return
			}
			value.ValueEncrypted, value.Value = encrypted, ""
		} else if creating {
			fail(c, 400, "value_required", "value is required")
			return
		}
	} else {
		value.Value, value.ValueEncrypted = in.Value, ""
	}
	var err error
	if creating {
		err = a.db.Create(&value).Error
	} else {
		err = a.db.Save(&value).Error
	}
	if err != nil {
		fail(c, 409, "environment_variable_exists", "environment variable already exists")
		return
	}
	status := 200
	if creating {
		status = 201
	}
	c.JSON(status, redactEnvironment(value))
}
func (a *App) saveGlobalEnvironmentVariable(c *gin.Context) {
	a.saveEnvironment(c, c.MustGet("userID").(uint), 0, c.Param("variableId"))
}
func (a *App) saveProjectEnvironmentVariable(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	a.saveEnvironment(c, project.UserID, id, c.Param("variableId"))
}
func (a *App) deleteEnvironment(c *gin.Context, ownerID, projectID uint, idParam string) {
	a.db.Where("id=? AND user_id=? AND project_id=?", idParam, ownerID, projectID).Delete(&EnvironmentVariable{})
	c.Status(204)
}
func (a *App) deleteGlobalEnvironmentVariable(c *gin.Context) {
	a.deleteEnvironment(c, c.MustGet("userID").(uint), 0, c.Param("variableId"))
}
func (a *App) deleteProjectEnvironmentVariable(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	a.deleteEnvironment(c, project.UserID, id, c.Param("variableId"))
}

func buildEnvironmentSnapshot(db *gorm.DB, cfg Config, userID, projectID uint) (string, error) {
	var values []EnvironmentVariable
	if err := db.Where("user_id=? AND project_id IN ?", userID, []uint{0, projectID}).Find(&values).Error; err != nil {
		return "", err
	}
	s := environmentSnapshot{Global: map[string]string{}, Project: map[string]string{}}
	for _, v := range values {
		value := v.Value
		if v.Sensitive {
			var err error
			value, err = decryptSecret(cfg.SecretEncryptionKey, v.ValueEncrypted)
			if err != nil {
				return "", err
			}
			s.Secrets = append(s.Secrets, value)
		}
		if v.ProjectID == 0 {
			s.Global[v.Name] = value
		} else {
			s.Project[v.Name] = value
		}
	}
	raw, _ := json.Marshal(s)
	if len(s.Secrets) > 0 {
		encrypted, err := encryptSecret(cfg.SecretEncryptionKey, string(raw))
		if err != nil {
			return "", err
		}
		return "enc:" + encrypted, nil
	}
	return string(raw), nil
}
func decodeEnvironmentSnapshot(cfg Config, raw string) (environmentSnapshot, error) {
	if strings.HasPrefix(raw, "enc:") {
		plain, err := decryptSecret(cfg.SecretEncryptionKey, strings.TrimPrefix(raw, "enc:"))
		if err != nil {
			return environmentSnapshot{}, err
		}
		raw = plain
	}
	s := environmentSnapshot{Global: map[string]string{}, Project: map[string]string{}}
	if raw == "" {
		return s, nil
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return s, fmt.Errorf("invalid environment snapshot: %w", err)
	}
	return s, nil
}
