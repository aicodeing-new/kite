package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	expirable "github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/zxh326/kite/pkg/common"
	"github.com/zxh326/kite/pkg/utils"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type User struct {
	Model
	Username    string      `json:"username" gorm:"type:varchar(50);uniqueIndex;not null"`
	Password    string      `json:"-" gorm:"type:varchar(255)"`
	Name        string      `json:"name,omitempty" gorm:"type:varchar(100);index"`
	AvatarURL   string      `json:"avatar_url,omitempty" gorm:"type:varchar(500)"`
	Provider    string      `json:"provider,omitempty" gorm:"type:varchar(50);default:password;index"`
	OIDCGroups  SliceString `json:"oidc_groups,omitempty" gorm:"type:text"`
	LastLoginAt *time.Time  `json:"lastLoginAt,omitempty" gorm:"type:timestamp;index"`
	Enabled     bool        `json:"enabled" gorm:"type:boolean;default:true"`
	Sub         string      `json:"sub,omitempty" gorm:"type:varchar(255);index"`

	APIKey SecretString  `json:"apiKey,omitempty" gorm:"type:text"`
	Roles  []common.Role `json:"roles,omitempty" gorm:"-"`
	Groups []UserGroup   `json:"groups,omitempty" gorm:"many2many:user_group_members"`

	SidebarPreference string `json:"sidebar_preference,omitempty" gorm:"size:16777215"`
	// DefaultCluster is the user's personal default cluster preference.
	// Empty means "follow the global default cluster".
	DefaultCluster string `json:"default_cluster,omitempty" gorm:"type:varchar(100)"`
}

func (u *User) Key() string {
	if u.Username != "" {
		return u.Username
	}
	if u.Name != "" {
		return u.Name
	}
	if u.Sub != "" {
		return u.Sub
	}
	return fmt.Sprintf("%d", u.ID)
}

func (u *User) GetAPIKey() string {
	return fmt.Sprintf("kite%d-%s", u.ID, string(u.APIKey))
}

func AddUser(user *User) error {
	// Hash the password before storing it
	hash, err := utils.HashPassword(user.Password)
	if err != nil {
		return err
	}
	user.Password = hash
	return DB.Create(user).Error
}

func CountUsers() (count int64, err error) {
	return count, DB.Model(&User{}).Count(&count).Error
}

// userCache is a thread-safe LRU with 30s TTL.
// Eliminates the per-request SELECT in RequireAuth (~1-5ms → ~50ns).
// Capacity 256 is generous for a K8s dashboard user base.
var userCache = expirable.NewLRU[uint64, *User](256, nil, 30*time.Second)

// GetUserByIDCached returns the user from cache if available, otherwise
// fetches from DB and stores it.  Used on the hot auth path.
func GetUserByIDCached(id uint64) (*User, error) {
	if u, ok := userCache.Get(id); ok {
		// Return a shallow copy so callers (RequireAuth, etc.) can safely
		// mutate fields like Roles without racing on the cached pointer.
		copy := *u
		return &copy, nil
	}
	u, err := GetUserByID(id)
	if err != nil {
		return nil, err
	}
	userCache.Add(id, u)
	// Also return a copy on miss path to keep the cached entry immutable.
	copy := *u
	return &copy, nil
}

// InvalidateUserCache removes a user from the auth cache.
// Called after every successful mutation so that security-sensitive changes
// (disable, delete, password reset) take effect on the next auth check.
func InvalidateUserCache(id uint64) {
	userCache.Remove(id)
}

func GetUserByID(id uint64) (*User, error) {
	var user User
	if err := DB.Where("id = ?", id).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func GetAnonymousUser() *User {
	user := &User{}
	if err := DB.Where("username = ? AND provider = ?", "anonymous", "Anonymous").First(user).Error; err != nil {
		return nil
	}
	return user
}

func FindWithSubOrUpsertUser(user *User) error {
	if user.Sub == "" {
		return errors.New("user sub is empty")
	}
	var existingUser User
	now := time.Now()
	user.LastLoginAt = &now
	if err := DB.Where("sub = ?", user.Sub).First(&existingUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Create new user
			if err := DB.Create(user).Error; err != nil {
				return err
			}
			// Auto-assign default role for OAuth users
			if err := assignOAuthDefaultRole(user); err != nil {
				klog.Warningf("Failed to assign default role to new OAuth user %s: %v", user.Username, err)
			}
			return nil
		}
		return err
	}
	user.Enabled = existingUser.Enabled

	user.ID = existingUser.ID
	user.CreatedAt = existingUser.CreatedAt
	user.SidebarPreference = existingUser.SidebarPreference
	user.DefaultCluster = existingUser.DefaultCluster

	// Log username changes for debugging
	if existingUser.Username != user.Username {
		klog.Infof("Updating user %d username: %q -> %q", user.ID, existingUser.Username, user.Username)
	}

	err := DB.Save(user).Error
	InvalidateUserCache(uint64(user.ID))
	return err
}

// assignOAuthDefaultRole assigns the default role to a new OAuth user if OAUTH_DEFAULT_ROLE is set.
func assignOAuthDefaultRole(user *User) error {
	if common.OAuthDefaultRole == "" {
		return nil // No default role configured
	}

	// Skip if user is not OAuth (password or api_key provider)
	if user.Provider == "password" || user.Provider == common.APIKeyProvider {
		return nil
	}

	// Find the role
	var role Role
	if err := DB.Where("name = ?", common.OAuthDefaultRole).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("default role %q not found", common.OAuthDefaultRole)
		}
		return fmt.Errorf("failed to find default role: %w", err)
	}

	// Check if assignment already exists
	var existingAssignment RoleAssignment
	err := DB.Where("role_id = ? AND subject_type = ? AND subject = ?",
		role.ID, SubjectTypeUser, user.Username).First(&existingAssignment).Error
	if err == nil {
		return nil // Assignment already exists
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check existing assignment: %w", err)
	}

	// Create role assignment
	assignment := RoleAssignment{
		RoleID:      role.ID,
		SubjectType: SubjectTypeUser,
		Subject:     user.Username,
	}
	if err := DB.Create(&assignment).Error; err != nil {
		return fmt.Errorf("failed to create role assignment: %w", err)
	}

	klog.Infof("Assigned default role %q to new OAuth user %s (provider: %s)", common.OAuthDefaultRole, user.Username, user.Provider)
	return nil
}

func GetUserByUsername(username string) (*User, error) {
	var user User
	if err := DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserRolesFromDB retrieves all roles assigned to a user from the database.
// This includes both direct user assignments and locally managed group assignments.
func GetUserRolesFromDB(username string) ([]common.Role, error) {
	if DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	localGroups := DB.Table("user_groups").
		Select("user_groups.name").
		Joins("JOIN user_group_members ON user_group_members.user_group_id = user_groups.id").
		Joins("JOIN users ON users.id = user_group_members.user_id").
		Where("users.username = ?", username)

	var results []Role
	err := DB.Table("roles").
		Select("DISTINCT roles.*").
		Joins("JOIN role_assignments ON role_assignments.role_id = roles.id").
		Where(
			"(role_assignments.subject_type = ? AND role_assignments.subject = ?) OR "+
				"(role_assignments.subject_type = ? AND role_assignments.subject IN (?))",
			SubjectTypeUser, username, SubjectTypeLocalGroup, localGroups,
		).
		Find(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query user roles: %w", err)
	}

	roles := make([]common.Role, 0, len(results))
	for _, r := range results {
		roles = append(roles, common.Role{
			Name:            r.Name,
			Description:     r.Description,
			Clusters:        r.Clusters,
			Resources:       r.Resources,
			ResourceNames:   r.ResourceNames,
			Namespaces:      r.Namespaces,
			Verbs:           r.Verbs,
			AllowProxy:      r.AllowProxy,
			ProxyNamespaces: r.ProxyNamespaces,
		})
	}

	return roles, nil
}

// ListUsers returns users with pagination. If limit is 0, defaults to 20.
func ListUsers(limit int, offset int, search string, sortBy string, sortOrder string, role string) (users []User, total int64, err error) {
	if limit <= 0 {
		limit = 20
	}
	query := DB.Model(&User{}).Where("users.provider != ?", common.APIKeyProvider)
	if role != "" {
		query = query.Where(`EXISTS (
			SELECT 1 FROM role_assignments ra
			JOIN roles r ON r.id = ra.role_id
			LEFT JOIN user_groups ug ON ra.subject_type = ? AND ra.subject = ug.name
			LEFT JOIN user_group_members ugm ON ugm.user_group_id = ug.id AND ugm.user_id = users.id
			WHERE r.name = ? AND (
				(ra.subject_type = ? AND ra.subject = users.username) OR
				(ra.subject_type = ? AND ugm.user_id IS NOT NULL)
			)
		)`, SubjectTypeLocalGroup, role, SubjectTypeUser, SubjectTypeLocalGroup)
	}
	if search != "" {
		likeQuery := "%" + search + "%"
		query = query.Where(
			"users.username LIKE ? OR users.name LIKE ?",
			likeQuery,
			likeQuery,
		)
	}
	// Use GROUP BY users.id instead of DISTINCT users.id so the sort column
	// can be included in the SELECT list. DISTINCT users.id would force
	// SELECT to only contain users.id, which then violates MySQL's
	// ONLY_FULL_GROUP_BY sql_mode when ORDER BY references another column
	// (Error 3065). GROUP BY on the primary key is functionally equivalent
	// for deduplication and, per MySQL semantics, allows selecting any
	// column of the same table without violating ONLY_FULL_GROUP_BY.
	countQuery := query.Select("users.id").Group("users.id")
	err = DB.Table("(?) as sub", countQuery).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}
	allowedSorts := map[string]string{
		"id":          "users.id",
		"createdAt":   "users.created_at",
		"lastLoginAt": "users.last_login_at",
	}
	sortColumn, ok := allowedSorts[sortBy]
	if !ok {
		sortColumn = "users.id"
	}
	orderExpr := fmt.Sprintf("%s %s", sortColumn, sortOrder)
	if sortColumn == "users.last_login_at" {
		orderExpr = fmt.Sprintf("users.last_login_at IS NULL, users.last_login_at %s", sortOrder)
	}
	// Select the sort column alongside users.id so the ORDER BY clause is
	// compatible with MySQL's ONLY_FULL_GROUP_BY sql_mode (enabled by default
	// on MySQL 5.7+). Without this, `SELECT DISTINCT users.id ... ORDER BY
	// users.last_login_at` raises "Error 3065: Expression #1 of ORDER BY
	// clause is not in SELECT list" and the whole /admin/users endpoint 500s.
	// We use GROUP BY users.id (not DISTINCT users.id) because DISTINCT
	// forces SELECT to only contain users.id, defeating the fix. GROUP BY
	// on the primary key deduplicates identically and, per MySQL semantics,
	// permits selecting any other column of the same table.
	idSelect := "users.id"
	if sortColumn != "users.id" {
		idSelect = fmt.Sprintf("users.id, %s", sortColumn)
	}
	type idRow struct {
		ID uint `gorm:"column:id"`
	}
	var idRows []idRow
	err = query.
		Select(idSelect).
		Group("users.id").
		Order(orderExpr).
		Limit(limit).
		Offset(offset).
		Scan(&idRows).Error
	if err != nil {
		return nil, 0, err
	}
	if len(idRows) == 0 {
		// Avoid `WHERE id IN ()` which is a syntax error on some drivers/DBs.
		return users, total, nil
	}
	userIds := make([]uint, 0, len(idRows))
	for _, r := range idRows {
		userIds = append(userIds, r.ID)
	}
	err = DB.
		Where("id IN (?)", userIds).
		Order(orderExpr).
		Find(&users).Error
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func LoginUser(u *User) error {
	now := time.Now()
	u.LastLoginAt = &now
	return DB.Save(u).Error
}

// DeleteUserByID removes a user by ID
func DeleteUserByID(id uint) error {
	_ = DB.Where("operator_id = ?", id).Delete(&ResourceHistory{}).Error
	_ = DB.Model(&User{Model: Model{ID: id}}).Association("Groups").Clear()
	err := DB.Delete(&User{}, id).Error
	InvalidateUserCache(uint64(id))
	return err
}

// UpdateUser saves provided user (expects ID set)
func UpdateUser(user *User) error {
	err := DB.Save(user).Error
	InvalidateUserCache(uint64(user.ID))
	return err
}

// ResetPasswordByID sets a new password (hashed) for user with given id
func ResetPasswordByID(id uint, plainPassword string) error {
	var u User
	if err := DB.First(&u, id).Error; err != nil {
		return err
	}
	hash, err := utils.HashPassword(plainPassword)
	if err != nil {
		return err
	}
	u.Password = hash
	err = DB.Save(&u).Error
	InvalidateUserCache(uint64(id))
	return err
}

// SetUserDefaultCluster stores a user's personal default cluster preference.
// An empty cluster name clears the preference so the global default applies.
func SetUserDefaultCluster(id uint, cluster string) error {
	err := DB.Model(&User{}).Where("id = ?", id).Update("default_cluster", cluster).Error
	InvalidateUserCache(uint64(id))
	return err
}

// SetUserEnabled sets enabled flag for a user
func SetUserEnabled(id uint, enabled bool) error {
	err := DB.Model(&User{}).Where("id = ?", id).Update("enabled", enabled).Error
	InvalidateUserCache(uint64(id))
	return err
}

func CheckPassword(hashedPassword, plainPassword string) bool {
	return utils.CheckPasswordHash(plainPassword, hashedPassword)
}

func UpsertLDAPUser(user *User) (*User, error) {
	if user == nil {
		return nil, errors.New("user is nil")
	}

	user.Username = strings.TrimSpace(user.Username)
	if user.Username == "" {
		return nil, errors.New("username is empty")
	}

	now := time.Now()
	user.Provider = AuthProviderLDAP
	user.Password = ""
	user.LastLoginAt = &now

	var existingUser User
	if err := DB.Where("username = ?", user.Username).First(&existingUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			user.Enabled = true
			if strings.TrimSpace(user.Name) == "" {
				user.Name = user.Username
			}
			err = DB.Create(user).Error
			if err == nil {
				InvalidateUserCache(uint64(user.ID))
				return user, nil
			}
			if !isUniqueConstraintError(err) {
				return nil, err
			}
			if err := DB.Where("username = ?", user.Username).First(&existingUser).Error; err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	if existingUser.Provider != AuthProviderLDAP {
		return nil, ErrUserProviderConflict
	}

	user.ID = existingUser.ID
	user.CreatedAt = existingUser.CreatedAt
	user.Enabled = existingUser.Enabled
	user.SidebarPreference = existingUser.SidebarPreference
	user.DefaultCluster = existingUser.DefaultCluster
	user.Sub = existingUser.Sub
	if strings.TrimSpace(user.Name) == "" {
		user.Name = existingUser.Name
	}
	if strings.TrimSpace(user.AvatarURL) == "" {
		user.AvatarURL = existingUser.AvatarURL
	}

	err := DB.Save(user).Error
	if err == nil {
		InvalidateUserCache(uint64(user.ID))
	}
	return user, err
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key value") ||
		strings.Contains(message, "duplicate entry")
}

func AddSuperUser(user *User) error {
	if user == nil {
		return errors.New("user is nil")
	}
	if err := AddUser(user); err != nil {
		return err
	}
	if err := AddRoleAssignment("admin", SubjectTypeUser, user.Username); err != nil {
		return err
	}
	return nil
}

func NewAPIKeyUser(name string) (*User, error) {
	apiKey := utils.RandomString(32)
	u := &User{
		Username: name,
		APIKey:   SecretString(apiKey),
		Provider: common.APIKeyProvider,
	}
	return u, DB.Save(u).Error
}

func ListAPIKeyUsers() (users []User, err error) {
	err = DB.Order("id desc").Where("provider = ?", common.APIKeyProvider).Find(&users).Error
	return users, err
}

var (
	ErrUserProviderConflict = errors.New("user exists with different provider")

	AnonymousUser = User{
		Model: Model{
			ID: 0,
		},
		Username: "anonymous",
		Provider: "Anonymous",
		Roles: []common.Role{
			{
				Name:       "admin",
				Clusters:   []string{"*"},
				Resources:  []string{"*"},
				Namespaces: []string{"*"},
				Verbs:      []string{"*"},
			},
		},
	}
)
