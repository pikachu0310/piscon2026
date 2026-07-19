package main

import (
	"bytes"
	"crypto/ecdsa"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/sqlx"
	jsoniter "github.com/json-iterator/go"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
)

const (
	sessionName                 = "isucondition_go"
	conditionLimit              = 20
	frontendContentsPath        = "../public"
	jiaJWTSigningKeyPath        = "../ec256-public.pem"
	defaultIconFilePath         = "../NoImage.jpg"
	defaultJIAServiceURL        = "http://localhost:5000"
	mysqlErrNumDuplicateEntry   = 1062
	conditionLevelInfo          = "info"
	conditionLevelWarning       = "warning"
	conditionLevelCritical      = "critical"
	scoreConditionLevelInfo     = 3
	scoreConditionLevelWarning  = 2
	scoreConditionLevelCritical = 1
	trendCacheTTL               = 100 * time.Millisecond
)

var (
	db                  *sqlx.DB
	sessionStore        sessions.Store
	mySQLConnectionData *MySQLConnectionEnv
	conditionJSON       = jsoniter.ConfigCompatibleWithStandardLibrary

	jiaJWTSigningKey *ecdsa.PublicKey

	postIsuConditionTargetBaseURL string // JIAへのactivate時に登録する，ISUがconditionを送る先のURL

	trendCache = struct {
		sync.Mutex
		body      []byte
		expiresAt time.Time
	}{}

	sessionCache = struct {
		sync.RWMutex
		users map[string]string
	}{users: make(map[string]string)}

	iconCache = struct {
		sync.RWMutex
		images map[string][]byte
	}{images: make(map[string][]byte)}

	isuMetadataCache = struct {
		sync.RWMutex
		byUUID map[string]CachedIsuMetadata
		byUser map[string][]CachedIsuMetadata
		loaded bool
	}{
		byUUID: make(map[string]CachedIsuMetadata),
		byUser: make(map[string][]CachedIsuMetadata),
	}

	conditionHistoryCache = struct {
		sync.RWMutex
		histories map[string]*ConditionHistory
		loaded    bool
	}{histories: make(map[string]*ConditionHistory)}

	canonicalConditionStrings = map[string]string{
		"is_dirty=false,is_overweight=false,is_broken=false": "is_dirty=false,is_overweight=false,is_broken=false",
		"is_dirty=true,is_overweight=false,is_broken=false":  "is_dirty=true,is_overweight=false,is_broken=false",
		"is_dirty=false,is_overweight=true,is_broken=false":  "is_dirty=false,is_overweight=true,is_broken=false",
		"is_dirty=true,is_overweight=true,is_broken=false":   "is_dirty=true,is_overweight=true,is_broken=false",
		"is_dirty=false,is_overweight=false,is_broken=true":  "is_dirty=false,is_overweight=false,is_broken=true",
		"is_dirty=true,is_overweight=false,is_broken=true":   "is_dirty=true,is_overweight=false,is_broken=true",
		"is_dirty=false,is_overweight=true,is_broken=true":   "is_dirty=false,is_overweight=true,is_broken=true",
		"is_dirty=true,is_overweight=true,is_broken=true":    "is_dirty=true,is_overweight=true,is_broken=true",
	}
	conditionMessageInterner sync.Map

	conditionWriteBarrier sync.RWMutex
)

type Config struct {
	Name string `db:"name"`
	URL  string `db:"url"`
}

type Isu struct {
	ID         int       `db:"id" json:"id"`
	JIAIsuUUID string    `db:"jia_isu_uuid" json:"jia_isu_uuid"`
	Name       string    `db:"name" json:"name"`
	Image      []byte    `db:"image" json:"-"`
	Character  string    `db:"character" json:"character"`
	JIAUserID  string    `db:"jia_user_id" json:"-"`
	CreatedAt  time.Time `db:"created_at" json:"-"`
	UpdatedAt  time.Time `db:"updated_at" json:"-"`
}

type IsuFromJIA struct {
	Character string `json:"character"`
}

type GetIsuListResponse struct {
	ID                 int                      `json:"id"`
	JIAIsuUUID         string                   `json:"jia_isu_uuid"`
	Name               string                   `json:"name"`
	Character          string                   `json:"character"`
	LatestIsuCondition *GetIsuConditionResponse `json:"latest_isu_condition"`
}

type IsuCondition struct {
	ID         int       `db:"id"`
	JIAIsuUUID string    `db:"jia_isu_uuid"`
	Timestamp  time.Time `db:"timestamp"`
	IsSitting  bool      `db:"is_sitting"`
	Condition  string    `db:"condition"`
	Message    string    `db:"message"`
	CreatedAt  time.Time `db:"created_at"`
}

type ConditionHistory struct {
	sync.RWMutex
	conditions []CachedCondition
}

type CachedCondition struct {
	Timestamp int64  `json:"timestamp"`
	Condition string `json:"condition"`
	Message   string `json:"message"`
	IsSitting bool   `json:"is_sitting"`
}

type MySQLConnectionEnv struct {
	Host     string
	Port     string
	User     string
	DBName   string
	Password string
}

type InitializeRequest struct {
	JIAServiceURL string `json:"jia_service_url"`
}

type InitializeResponse struct {
	Language string `json:"language"`
}

type GetMeResponse struct {
	JIAUserID string `json:"jia_user_id"`
}

type GraphResponse struct {
	StartAt             int64           `json:"start_at"`
	EndAt               int64           `json:"end_at"`
	Data                *GraphDataPoint `json:"data"`
	ConditionTimestamps []int64         `json:"condition_timestamps"`
}

type GraphDataPoint struct {
	Score      int                  `json:"score"`
	Percentage ConditionsPercentage `json:"percentage"`
}

type ConditionsPercentage struct {
	Sitting      int `json:"sitting"`
	IsBroken     int `json:"is_broken"`
	IsDirty      int `json:"is_dirty"`
	IsOverweight int `json:"is_overweight"`
}

type GraphDataPointWithInfo struct {
	JIAIsuUUID          string
	StartAt             time.Time
	Data                GraphDataPoint
	ConditionTimestamps []int64
}

type GetIsuConditionResponse struct {
	JIAIsuUUID     string `json:"jia_isu_uuid"`
	IsuName        string `json:"isu_name"`
	Timestamp      int64  `json:"timestamp"`
	IsSitting      bool   `json:"is_sitting"`
	Condition      string `json:"condition"`
	ConditionLevel string `json:"condition_level"`
	Message        string `json:"message"`
}

type TrendResponse struct {
	Character string            `json:"character"`
	Info      []*TrendCondition `json:"info"`
	Warning   []*TrendCondition `json:"warning"`
	Critical  []*TrendCondition `json:"critical"`
}

type TrendCondition struct {
	ID        int   `json:"isu_id"`
	Timestamp int64 `json:"timestamp"`
}

type TrendQueryRow struct {
	ID         int    `db:"id"`
	JIAIsuUUID string `db:"jia_isu_uuid"`
	Character  string `db:"character"`
}

type IsuListQueryRow struct {
	ID         int    `db:"id"`
	JIAIsuUUID string `db:"jia_isu_uuid"`
	Name       string `db:"name"`
	Character  string `db:"character"`
}

type CachedIsuMetadata struct {
	ID         int    `db:"id"`
	JIAIsuUUID string `db:"jia_isu_uuid"`
	Name       string `db:"name"`
	Character  string `db:"character"`
	JIAUserID  string `db:"jia_user_id"`
}

type JIAServiceRequest struct {
	TargetBaseURL string `json:"target_base_url"`
	IsuUUID       string `json:"isu_uuid"`
}

func getEnv(key string, defaultValue string) string {
	val := os.Getenv(key)
	if val != "" {
		return val
	}
	return defaultValue
}

func NewMySQLConnectionEnv() *MySQLConnectionEnv {
	return &MySQLConnectionEnv{
		Host:     getEnv("MYSQL_HOST", "127.0.0.1"),
		Port:     getEnv("MYSQL_PORT", "3306"),
		User:     getEnv("MYSQL_USER", "isucon"),
		DBName:   getEnv("MYSQL_DBNAME", "isucondition"),
		Password: getEnv("MYSQL_PASS", "isucon"),
	}
}

func (mc *MySQLConnectionEnv) ConnectDB() (*sqlx.DB, error) {
	dsn := fmt.Sprintf("%v:%v@tcp(%v:%v)/%v?parseTime=true&loc=Asia%%2FTokyo&interpolateParams=true", mc.User, mc.Password, mc.Host, mc.Port, mc.DBName)
	return sqlx.Open("mysql", dsn)
}

func init() {
	sessionStore = sessions.NewCookieStore([]byte(getEnv("SESSION_KEY", "isucondition")))

	key, err := ioutil.ReadFile(jiaJWTSigningKeyPath)
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
	}
	jiaJWTSigningKey, err = jwt.ParseECPublicKeyFromPEM(key)
	if err != nil {
		log.Fatalf("failed to parse ECDSA public key: %v", err)
	}
}

func main() {
	e := echo.New()
	e.Debug = false
	e.Logger.SetLevel(log.INFO)

	e.Use(middleware.Recover())
	e.Use(profilingMiddleware)

	e.POST("/initialize", postInitialize)

	e.POST("/api/auth", postAuthentication)
	e.POST("/api/signout", postSignout)
	e.GET("/api/user/me", getMe)
	e.GET("/api/isu", getIsuList)
	e.POST("/api/isu", postIsu)
	e.GET("/api/isu/:jia_isu_uuid", getIsuID)
	e.GET("/api/isu/:jia_isu_uuid/icon", getIsuIcon)
	e.GET("/api/isu/:jia_isu_uuid/graph", getIsuGraph)
	e.GET("/api/condition/:jia_isu_uuid", getIsuConditions)
	e.GET("/api/trend", getTrend)

	e.POST("/api/condition/:jia_isu_uuid", postIsuCondition)

	e.GET("/", getIndex)
	e.GET("/isu/:jia_isu_uuid", getIndex)
	e.GET("/isu/:jia_isu_uuid/condition", getIndex)
	e.GET("/isu/:jia_isu_uuid/graph", getIndex)
	e.GET("/register", getIndex)
	e.Static("/assets", frontendContentsPath+"/assets")

	mySQLConnectionData = NewMySQLConnectionEnv()

	var err error
	db, err = mySQLConnectionData.ConnectDB()
	if err != nil {
		e.Logger.Fatalf("failed to connect db: %v", err)
		return
	}
	db.SetMaxOpenConns(64)
	db.SetMaxIdleConns(64)
	if err = reloadConditionHistories(); err != nil {
		e.Logger.Fatalf("failed to load condition histories: %v", err)
		return
	}
	defer db.Close()
	startDiagnosticsServer(e.Logger)

	postIsuConditionTargetBaseURL = os.Getenv("POST_ISUCONDITION_TARGET_BASE_URL")
	if postIsuConditionTargetBaseURL == "" {
		e.Logger.Fatalf("missing: POST_ISUCONDITION_TARGET_BASE_URL")
		return
	}

	serverPort := fmt.Sprintf(":%v", getEnv("SERVER_APP_PORT", "3000"))
	e.Logger.Fatal(e.Start(serverPort))
}

func getSession(r *http.Request) (*sessions.Session, error) {
	session, err := sessionStore.Get(r, sessionName)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func invalidateTrendCache() {
	trendCache.Lock()
	trendCache.body = nil
	trendCache.expiresAt = time.Time{}
	trendCache.Unlock()
}

func clearSessionCache() {
	sessionCache.Lock()
	sessionCache.users = make(map[string]string)
	sessionCache.Unlock()
}

func deleteCachedSession(r *http.Request) {
	cookie, err := r.Cookie(sessionName)
	if err != nil {
		return
	}
	sessionCache.Lock()
	delete(sessionCache.users, cookie.Value)
	sessionCache.Unlock()
}

func iconCacheKey(jiaUserID string, jiaIsuUUID string) string {
	return jiaUserID + "\x00" + jiaIsuUUID
}

func clearIconCache() {
	iconCache.Lock()
	iconCache.images = make(map[string][]byte)
	iconCache.Unlock()
}

func cacheIsuIcon(jiaUserID string, jiaIsuUUID string, image []byte) {
	iconCache.Lock()
	iconCache.images[iconCacheKey(jiaUserID, jiaIsuUUID)] = image
	iconCache.Unlock()
}

func reloadKnownIsus() error {
	var rows []CachedIsuMetadata
	if err := db.Select(&rows,
		"SELECT `id`, `jia_isu_uuid`, `name`, `character`, `jia_user_id` FROM `isu`"); err != nil {
		return err
	}

	byUUID := make(map[string]CachedIsuMetadata, len(rows))
	byUser := make(map[string][]CachedIsuMetadata)
	for _, metadata := range rows {
		byUUID[metadata.JIAIsuUUID] = metadata
		byUser[metadata.JIAUserID] = append(byUser[metadata.JIAUserID], metadata)
	}
	for userID := range byUser {
		sort.Slice(byUser[userID], func(i, j int) bool {
			return byUser[userID][i].ID > byUser[userID][j].ID
		})
	}

	isuMetadataCache.Lock()
	isuMetadataCache.byUUID = byUUID
	isuMetadataCache.byUser = byUser
	isuMetadataCache.loaded = true
	isuMetadataCache.Unlock()
	return nil
}

func cacheIsuMetadata(metadata CachedIsuMetadata) {
	isuMetadataCache.Lock()
	isuMetadataCache.byUUID[metadata.JIAIsuUUID] = metadata
	userIsus := append(isuMetadataCache.byUser[metadata.JIAUserID], metadata)
	sort.Slice(userIsus, func(i, j int) bool { return userIsus[i].ID > userIsus[j].ID })
	isuMetadataCache.byUser[metadata.JIAUserID] = userIsus
	isuMetadataCache.Unlock()
}

func getCachedIsuMetadata(jiaIsuUUID string) (CachedIsuMetadata, bool, bool) {
	isuMetadataCache.RLock()
	metadata, ok := isuMetadataCache.byUUID[jiaIsuUUID]
	loaded := isuMetadataCache.loaded
	isuMetadataCache.RUnlock()
	return metadata, ok, loaded
}

func getCachedOwnedIsuMetadata(jiaUserID, jiaIsuUUID string) (CachedIsuMetadata, bool, bool) {
	metadata, ok, loaded := getCachedIsuMetadata(jiaIsuUUID)
	if ok && metadata.JIAUserID != jiaUserID {
		ok = false
	}
	return metadata, ok, loaded
}

func getCachedIsuList(jiaUserID string) ([]IsuListQueryRow, bool) {
	isuMetadataCache.RLock()
	loaded := isuMetadataCache.loaded
	metadata := isuMetadataCache.byUser[jiaUserID]
	rows := make([]IsuListQueryRow, len(metadata))
	for i, isu := range metadata {
		rows[i] = IsuListQueryRow{
			ID: isu.ID, JIAIsuUUID: isu.JIAIsuUUID, Name: isu.Name, Character: isu.Character,
		}
	}
	isuMetadataCache.RUnlock()
	return rows, loaded
}

func snapshotIsuMetadata() ([]TrendQueryRow, []string, bool) {
	isuMetadataCache.RLock()
	loaded := isuMetadataCache.loaded
	rows := make([]TrendQueryRow, 0, len(isuMetadataCache.byUUID))
	characters := make(map[string]struct{})
	for _, isu := range isuMetadataCache.byUUID {
		rows = append(rows, TrendQueryRow{
			ID: isu.ID, JIAIsuUUID: isu.JIAIsuUUID, Character: isu.Character,
		})
		characters[isu.Character] = struct{}{}
	}
	isuMetadataCache.RUnlock()

	characterList := make([]string, 0, len(characters))
	for character := range characters {
		characterList = append(characterList, character)
	}
	sort.Strings(characterList)
	return rows, characterList, loaded
}

func isKnownIsu(jiaIsuUUID string) (bool, error) {
	_, ok, loaded := getCachedIsuMetadata(jiaIsuUUID)
	if loaded {
		return ok, nil
	}

	// Before the first initialize after a process restart, preserve the original
	// behavior by consulting the database instead of trusting an empty cache.
	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM `isu` WHERE `jia_isu_uuid` = ?", jiaIsuUUID); err != nil {
		return false, err
	}
	return count != 0, nil
}

func snapshotLatestConditions() map[string]CachedCondition {
	conditionHistoryCache.RLock()
	histories := make(map[string]*ConditionHistory, len(conditionHistoryCache.histories))
	for uuid, history := range conditionHistoryCache.histories {
		histories[uuid] = history
	}
	conditionHistoryCache.RUnlock()

	conditions := make(map[string]CachedCondition, len(histories))
	for uuid, history := range histories {
		history.RLock()
		if len(history.conditions) != 0 {
			latest := history.conditions[len(history.conditions)-1]
			conditions[uuid] = latest
		}
		history.RUnlock()
	}
	return conditions
}

func reloadConditionHistories() error {
	rows, err := db.Queryx(
		"SELECT `jia_isu_uuid`, `timestamp`, `is_sitting`, `condition`, `message`" +
			" FROM `isu_condition` ORDER BY `jia_isu_uuid`, `timestamp`, `id`")
	if err != nil {
		return err
	}
	defer rows.Close()

	histories := make(map[string]*ConditionHistory)
	for rows.Next() {
		var condition IsuCondition
		if err = rows.StructScan(&condition); err != nil {
			return err
		}
		history := histories[condition.JIAIsuUUID]
		if history == nil {
			history = &ConditionHistory{}
			histories[condition.JIAIsuUUID] = history
		}
		canonical, ok := canonicalConditionStrings[condition.Condition]
		if !ok {
			return fmt.Errorf("invalid condition in baseline: %q", condition.Condition)
		}
		history.conditions = append(history.conditions, CachedCondition{
			Timestamp: condition.Timestamp.Unix(),
			Condition: canonical,
			Message:   internConditionMessage(condition.Message),
			IsSitting: condition.IsSitting,
		})
	}
	if err = rows.Err(); err != nil {
		return err
	}

	conditionHistoryCache.Lock()
	conditionHistoryCache.histories = histories
	conditionHistoryCache.loaded = true
	conditionHistoryCache.Unlock()
	return nil
}

func internConditionMessage(message string) string {
	value, _ := conditionMessageInterner.LoadOrStore(message, message)
	return value.(string)
}

func getOrCreateConditionHistory(jiaIsuUUID string) *ConditionHistory {
	conditionHistoryCache.RLock()
	history := conditionHistoryCache.histories[jiaIsuUUID]
	conditionHistoryCache.RUnlock()
	if history != nil {
		return history
	}

	conditionHistoryCache.Lock()
	defer conditionHistoryCache.Unlock()
	history = conditionHistoryCache.histories[jiaIsuUUID]
	if history == nil {
		history = &ConditionHistory{}
		conditionHistoryCache.histories[jiaIsuUUID] = history
	}
	return history
}

func cacheConditionHistory(jiaIsuUUID string, conditions []CachedCondition) {
	history := getOrCreateConditionHistory(jiaIsuUUID)
	sort.SliceStable(conditions, func(i, j int) bool {
		return conditions[i].Timestamp < conditions[j].Timestamp
	})

	history.Lock()
	needsSort := len(history.conditions) > 0 && len(conditions) > 0 &&
		conditions[0].Timestamp < history.conditions[len(history.conditions)-1].Timestamp
	history.conditions = append(history.conditions, conditions...)
	if needsSort {
		sort.SliceStable(history.conditions, func(i, j int) bool {
			return history.conditions[i].Timestamp < history.conditions[j].Timestamp
		})
	}
	history.Unlock()
}

func conditionHistoryRange(jiaIsuUUID string, startTime, endTime time.Time) ([]CachedCondition, bool) {
	conditionHistoryCache.RLock()
	loaded := conditionHistoryCache.loaded
	history := conditionHistoryCache.histories[jiaIsuUUID]
	conditionHistoryCache.RUnlock()
	if !loaded {
		return nil, false
	}
	if history == nil {
		return []CachedCondition{}, true
	}

	history.RLock()
	defer history.RUnlock()
	startUnix := startTime.Unix()
	endUnix := endTime.Unix()
	start := sort.Search(len(history.conditions), func(i int) bool {
		return history.conditions[i].Timestamp >= startUnix
	})
	end := sort.Search(len(history.conditions), func(i int) bool {
		return history.conditions[i].Timestamp >= endUnix
	})
	conditions := make([]CachedCondition, end-start)
	copy(conditions, history.conditions[start:end])
	return conditions, true
}

func getUserIDFromSession(c echo.Context) (string, int, error) {
	cookie, cookieErr := c.Request().Cookie(sessionName)
	if cookieErr == nil {
		sessionCache.RLock()
		jiaUserID, ok := sessionCache.users[cookie.Value]
		sessionCache.RUnlock()
		if ok {
			return jiaUserID, 0, nil
		}
	}

	session, err := getSession(c.Request())
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("failed to get session: %v", err)
	}
	_jiaUserID, ok := session.Values["jia_user_id"]
	if !ok {
		return "", http.StatusUnauthorized, fmt.Errorf("no session")
	}

	jiaUserID, ok := _jiaUserID.(string)
	if !ok {
		return "", http.StatusUnauthorized, fmt.Errorf("invalid session")
	}
	var count int

	err = db.Get(&count, "SELECT COUNT(*) FROM `user` WHERE `jia_user_id` = ?",
		jiaUserID)
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("db error: %v", err)
	}

	if count == 0 {
		return "", http.StatusUnauthorized, fmt.Errorf("not found: user")
	}

	if cookieErr == nil {
		sessionCache.Lock()
		sessionCache.users[cookie.Value] = jiaUserID
		sessionCache.Unlock()
	}

	return jiaUserID, 0, nil
}

func getJIAServiceURL(tx *sqlx.Tx) string {
	var config Config
	err := tx.Get(&config, "SELECT * FROM `isu_association_config` WHERE `name` = ?", "jia_service_url")
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Print(err)
		}
		return defaultJIAServiceURL
	}
	return config.URL
}

// POST /initialize
// サービスを初期化
func postInitialize(c echo.Context) error {
	var request InitializeRequest
	err := c.Bind(&request)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad request body")
	}

	// Stop condition updates while the DB and its memory snapshots are replaced.
	conditionWriteBarrier.Lock()
	defer conditionWriteBarrier.Unlock()

	cmd := exec.Command("../sql/init.sh")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	err = cmd.Run()
	if err != nil {
		c.Logger().Errorf("exec init.sh error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	if err = reloadConditionHistories(); err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	_, err = db.Exec(
		"INSERT INTO `isu_association_config` (`name`, `url`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `url` = VALUES(`url`)",
		"jia_service_url",
		request.JIAServiceURL,
	)
	if err != nil {
		c.Logger().Errorf("db error : %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	if err = reloadKnownIsus(); err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	clearSessionCache()
	clearIconCache()
	invalidateTrendCache()
	notifyInitializeCapture()
	return c.JSON(http.StatusOK, InitializeResponse{
		Language: "go",
	})
}

// POST /api/auth
// サインアップ・サインイン
func postAuthentication(c echo.Context) error {
	reqJwt := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")

	token, err := jwt.Parse(reqJwt, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, jwt.NewValidationError(fmt.Sprintf("unexpected signing method: %v", token.Header["alg"]), jwt.ValidationErrorSignatureInvalid)
		}
		return jiaJWTSigningKey, nil
	})
	if err != nil {
		switch err.(type) {
		case *jwt.ValidationError:
			return c.String(http.StatusForbidden, "forbidden")
		default:
			c.Logger().Error(err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.Logger().Errorf("invalid JWT payload")
		return c.NoContent(http.StatusInternalServerError)
	}
	jiaUserIDVar, ok := claims["jia_user_id"]
	if !ok {
		return c.String(http.StatusBadRequest, "invalid JWT payload")
	}
	jiaUserID, ok := jiaUserIDVar.(string)
	if !ok {
		return c.String(http.StatusBadRequest, "invalid JWT payload")
	}

	_, err = db.Exec("INSERT IGNORE INTO user (`jia_user_id`) VALUES (?)", jiaUserID)
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	session, err := getSession(c.Request())
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	session.Values["jia_user_id"] = jiaUserID
	err = session.Save(c.Request(), c.Response())
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	deleteCachedSession(c.Request())
	return c.NoContent(http.StatusOK)
}

// POST /api/signout
// サインアウト
func postSignout(c echo.Context) error {
	_, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	session, err := getSession(c.Request())
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	session.Options = &sessions.Options{MaxAge: -1, Path: "/"}
	err = session.Save(c.Request(), c.Response())
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	deleteCachedSession(c.Request())
	return c.NoContent(http.StatusOK)
}

// GET /api/user/me
// サインインしている自分自身の情報を取得
func getMe(c echo.Context) error {
	jiaUserID, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	res := GetMeResponse{JIAUserID: jiaUserID}
	return c.JSON(http.StatusOK, res)
}

// GET /api/isu
// ISUの一覧を取得
func getIsuList(c echo.Context) error {
	jiaUserID, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	isuList, loaded := getCachedIsuList(jiaUserID)
	if !loaded {
		isuList = []IsuListQueryRow{}
		err = db.Select(
			&isuList,
			"SELECT i.`id`, i.`jia_isu_uuid`, i.`name`, i.`character`"+
				" FROM `isu` i"+
				" WHERE i.`jia_user_id` = ? ORDER BY i.`id` DESC",
			jiaUserID)
		if err != nil {
			c.Logger().Errorf("db error: %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	responseList := []GetIsuListResponse{}
	latestConditions := snapshotLatestConditions()
	for _, isu := range isuList {
		var formattedCondition *GetIsuConditionResponse
		latestCondition, ok := latestConditions[isu.JIAIsuUUID]
		if ok {
			conditionLevel, err := calculateConditionLevel(latestCondition.Condition)
			if err != nil {
				c.Logger().Error(err)
				return c.NoContent(http.StatusInternalServerError)
			}

			formattedCondition = &GetIsuConditionResponse{
				JIAIsuUUID:     isu.JIAIsuUUID,
				IsuName:        isu.Name,
				Timestamp:      latestCondition.Timestamp,
				IsSitting:      latestCondition.IsSitting,
				Condition:      latestCondition.Condition,
				ConditionLevel: conditionLevel,
				Message:        latestCondition.Message,
			}
		}

		res := GetIsuListResponse{
			ID:                 isu.ID,
			JIAIsuUUID:         isu.JIAIsuUUID,
			Name:               isu.Name,
			Character:          isu.Character,
			LatestIsuCondition: formattedCondition}
		responseList = append(responseList, res)
	}

	return c.JSON(http.StatusOK, responseList)
}

// POST /api/isu
// ISUを登録
func postIsu(c echo.Context) error {
	jiaUserID, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	useDefaultImage := false

	jiaIsuUUID := c.FormValue("jia_isu_uuid")
	isuName := c.FormValue("isu_name")
	fh, err := c.FormFile("image")
	if err != nil {
		if !errors.Is(err, http.ErrMissingFile) {
			return c.String(http.StatusBadRequest, "bad format: icon")
		}
		useDefaultImage = true
	}

	var image []byte

	if useDefaultImage {
		image, err = ioutil.ReadFile(defaultIconFilePath)
		if err != nil {
			c.Logger().Error(err)
			return c.NoContent(http.StatusInternalServerError)
		}
	} else {
		file, err := fh.Open()
		if err != nil {
			c.Logger().Error(err)
			return c.NoContent(http.StatusInternalServerError)
		}
		defer file.Close()

		image, err = ioutil.ReadAll(file)
		if err != nil {
			c.Logger().Error(err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	tx, err := db.Beginx()
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer tx.Rollback()

	_, err = tx.Exec("INSERT INTO `isu`"+
		"	(`jia_isu_uuid`, `name`, `image`, `jia_user_id`) VALUES (?, ?, ?, ?)",
		jiaIsuUUID, isuName, image, jiaUserID)
	if err != nil {
		mysqlErr, ok := err.(*mysql.MySQLError)

		if ok && mysqlErr.Number == uint16(mysqlErrNumDuplicateEntry) {
			return c.String(http.StatusConflict, "duplicated: isu")
		}

		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	targetURL := getJIAServiceURL(tx) + "/api/activate"
	body := JIAServiceRequest{postIsuConditionTargetBaseURL, jiaIsuUUID}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	reqJIA, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewBuffer(bodyJSON))
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	reqJIA.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(reqJIA)
	if err != nil {
		c.Logger().Errorf("failed to request to JIAService: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer res.Body.Close()

	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	if res.StatusCode != http.StatusAccepted {
		c.Logger().Errorf("JIAService returned error: status code %v, message: %v", res.StatusCode, string(resBody))
		return c.String(res.StatusCode, "JIAService returned error")
	}

	var isuFromJIA IsuFromJIA
	err = json.Unmarshal(resBody, &isuFromJIA)
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	_, err = tx.Exec("UPDATE `isu` SET `character` = ? WHERE  `jia_isu_uuid` = ?", isuFromJIA.Character, jiaIsuUUID)
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	var isu Isu
	err = tx.Get(
		&isu,
		"SELECT `id`, `jia_isu_uuid`, `name`, `character` FROM `isu` WHERE `jia_user_id` = ? AND `jia_isu_uuid` = ?",
		jiaUserID, jiaIsuUUID)
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	err = tx.Commit()
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	cacheIsuMetadata(CachedIsuMetadata{
		ID: isu.ID, JIAIsuUUID: isu.JIAIsuUUID, Name: isu.Name,
		Character: isu.Character, JIAUserID: jiaUserID,
	})
	cacheIsuIcon(jiaUserID, jiaIsuUUID, image)
	invalidateTrendCache()
	return c.JSON(http.StatusCreated, isu)
}

// GET /api/isu/:jia_isu_uuid
// ISUの情報を取得
func getIsuID(c echo.Context) error {
	jiaUserID, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	jiaIsuUUID := c.Param("jia_isu_uuid")

	var res Isu
	metadata, found, loaded := getCachedOwnedIsuMetadata(jiaUserID, jiaIsuUUID)
	if loaded {
		if !found {
			return c.String(http.StatusNotFound, "not found: isu")
		}
		res = Isu{
			ID: metadata.ID, JIAIsuUUID: metadata.JIAIsuUUID,
			Name: metadata.Name, Character: metadata.Character,
		}
	} else {
		err = db.Get(&res, "SELECT `id`, `jia_isu_uuid`, `name`, `character` FROM `isu` WHERE `jia_user_id` = ? AND `jia_isu_uuid` = ?",
			jiaUserID, jiaIsuUUID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.String(http.StatusNotFound, "not found: isu")
			}

			c.Logger().Errorf("db error: %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	return c.JSON(http.StatusOK, res)
}

// GET /api/isu/:jia_isu_uuid/icon
// ISUのアイコンを取得
func getIsuIcon(c echo.Context) error {
	jiaUserID, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	jiaIsuUUID := c.Param("jia_isu_uuid")
	cacheKey := iconCacheKey(jiaUserID, jiaIsuUUID)
	iconCache.RLock()
	image, ok := iconCache.images[cacheKey]
	iconCache.RUnlock()
	if ok {
		return c.Blob(http.StatusOK, "", image)
	}

	err = db.Get(&image, "SELECT `image` FROM `isu` WHERE `jia_user_id` = ? AND `jia_isu_uuid` = ?",
		jiaUserID, jiaIsuUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.String(http.StatusNotFound, "not found: isu")
		}

		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	cacheIsuIcon(jiaUserID, jiaIsuUUID, image)
	return c.Blob(http.StatusOK, "", image)
}

// GET /api/isu/:jia_isu_uuid/graph
// ISUのコンディショングラフ描画のための情報を取得
func getIsuGraph(c echo.Context) error {
	jiaUserID, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	jiaIsuUUID := c.Param("jia_isu_uuid")
	datetimeStr := c.QueryParam("datetime")
	if datetimeStr == "" {
		return c.String(http.StatusBadRequest, "missing: datetime")
	}
	datetimeInt64, err := strconv.ParseInt(datetimeStr, 10, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad format: datetime")
	}
	date := time.Unix(datetimeInt64, 0).Truncate(time.Hour)

	_, found, loaded := getCachedOwnedIsuMetadata(jiaUserID, jiaIsuUUID)
	if loaded {
		if !found {
			return c.String(http.StatusNotFound, "not found: isu")
		}
	} else {
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM `isu` WHERE `jia_user_id` = ? AND `jia_isu_uuid` = ?",
			jiaUserID, jiaIsuUUID)
		if err != nil {
			c.Logger().Errorf("db error: %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
		if count == 0 {
			return c.String(http.StatusNotFound, "not found: isu")
		}
	}

	res, err := generateIsuGraphResponse(db, jiaIsuUUID, date)
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, res)
}

// グラフのデータ点を一日分生成
func generateIsuGraphResponse(db *sqlx.DB, jiaIsuUUID string, graphDate time.Time) ([]GraphResponse, error) {
	dataPoints := []GraphDataPointWithInfo{}
	conditionsInThisHour := []CachedCondition{}
	timestampsInThisHour := []int64{}
	var startTimeInThisHour time.Time

	conditions, loaded := conditionHistoryRange(jiaIsuUUID, graphDate, graphDate.Add(24*time.Hour))
	if !loaded {
		conditions = []CachedCondition{}
		rows, err := db.Queryx(
			"SELECT `jia_isu_uuid`, `timestamp`, `is_sitting`, `condition`, `message` FROM `isu_condition`"+
				" WHERE `jia_isu_uuid` = ? AND `timestamp` >= ? AND `timestamp` < ? ORDER BY `timestamp` ASC",
			jiaIsuUUID, graphDate, graphDate.Add(24*time.Hour))
		if err != nil {
			return nil, fmt.Errorf("db error: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var condition IsuCondition
			if err = rows.StructScan(&condition); err != nil {
				return nil, err
			}
			canonical, ok := canonicalConditionStrings[condition.Condition]
			if !ok {
				return nil, fmt.Errorf("invalid condition: %q", condition.Condition)
			}
			conditions = append(conditions, CachedCondition{
				Timestamp: condition.Timestamp.Unix(),
				Condition: canonical,
				Message:   internConditionMessage(condition.Message),
				IsSitting: condition.IsSitting,
			})
		}
	}

	for _, condition := range conditions {
		truncatedConditionTime := time.Unix(condition.Timestamp, 0).Truncate(time.Hour)
		if truncatedConditionTime != startTimeInThisHour {
			if len(conditionsInThisHour) > 0 {
				data, err := calculateGraphDataPoint(conditionsInThisHour)
				if err != nil {
					return nil, err
				}

				dataPoints = append(dataPoints,
					GraphDataPointWithInfo{
						JIAIsuUUID:          jiaIsuUUID,
						StartAt:             startTimeInThisHour,
						Data:                data,
						ConditionTimestamps: timestampsInThisHour})
			}

			startTimeInThisHour = truncatedConditionTime
			conditionsInThisHour = []CachedCondition{}
			timestampsInThisHour = []int64{}
		}
		conditionsInThisHour = append(conditionsInThisHour, condition)
		timestampsInThisHour = append(timestampsInThisHour, condition.Timestamp)
	}

	if len(conditionsInThisHour) > 0 {
		data, err := calculateGraphDataPoint(conditionsInThisHour)
		if err != nil {
			return nil, err
		}

		dataPoints = append(dataPoints,
			GraphDataPointWithInfo{
				JIAIsuUUID:          jiaIsuUUID,
				StartAt:             startTimeInThisHour,
				Data:                data,
				ConditionTimestamps: timestampsInThisHour})
	}

	endTime := graphDate.Add(time.Hour * 24)
	startIndex := len(dataPoints)
	endNextIndex := len(dataPoints)
	for i, graph := range dataPoints {
		if startIndex == len(dataPoints) && !graph.StartAt.Before(graphDate) {
			startIndex = i
		}
		if endNextIndex == len(dataPoints) && graph.StartAt.After(endTime) {
			endNextIndex = i
		}
	}

	filteredDataPoints := []GraphDataPointWithInfo{}
	if startIndex < endNextIndex {
		filteredDataPoints = dataPoints[startIndex:endNextIndex]
	}

	responseList := []GraphResponse{}
	index := 0
	thisTime := graphDate

	for thisTime.Before(graphDate.Add(time.Hour * 24)) {
		var data *GraphDataPoint
		timestamps := []int64{}

		if index < len(filteredDataPoints) {
			dataWithInfo := filteredDataPoints[index]

			if dataWithInfo.StartAt.Equal(thisTime) {
				data = &dataWithInfo.Data
				timestamps = dataWithInfo.ConditionTimestamps
				index++
			}
		}

		resp := GraphResponse{
			StartAt:             thisTime.Unix(),
			EndAt:               thisTime.Add(time.Hour).Unix(),
			Data:                data,
			ConditionTimestamps: timestamps,
		}
		responseList = append(responseList, resp)

		thisTime = thisTime.Add(time.Hour)
	}

	return responseList, nil
}

// 複数のISUのコンディションからグラフの一つのデータ点を計算
func calculateGraphDataPoint(isuConditions []CachedCondition) (GraphDataPoint, error) {
	conditionsCount := map[string]int{"is_broken": 0, "is_dirty": 0, "is_overweight": 0}
	rawScore := 0
	for _, condition := range isuConditions {
		badConditionsCount := 0

		if !isValidConditionFormat(condition.Condition) {
			return GraphDataPoint{}, fmt.Errorf("invalid condition format")
		}

		for _, condStr := range strings.Split(condition.Condition, ",") {
			keyValue := strings.Split(condStr, "=")

			conditionName := keyValue[0]
			if keyValue[1] == "true" {
				conditionsCount[conditionName] += 1
				badConditionsCount++
			}
		}

		if badConditionsCount >= 3 {
			rawScore += scoreConditionLevelCritical
		} else if badConditionsCount >= 1 {
			rawScore += scoreConditionLevelWarning
		} else {
			rawScore += scoreConditionLevelInfo
		}
	}

	sittingCount := 0
	for _, condition := range isuConditions {
		if condition.IsSitting {
			sittingCount++
		}
	}

	isuConditionsLength := len(isuConditions)

	score := rawScore * 100 / 3 / isuConditionsLength

	sittingPercentage := sittingCount * 100 / isuConditionsLength
	isBrokenPercentage := conditionsCount["is_broken"] * 100 / isuConditionsLength
	isOverweightPercentage := conditionsCount["is_overweight"] * 100 / isuConditionsLength
	isDirtyPercentage := conditionsCount["is_dirty"] * 100 / isuConditionsLength

	dataPoint := GraphDataPoint{
		Score: score,
		Percentage: ConditionsPercentage{
			Sitting:      sittingPercentage,
			IsBroken:     isBrokenPercentage,
			IsOverweight: isOverweightPercentage,
			IsDirty:      isDirtyPercentage,
		},
	}
	return dataPoint, nil
}

// GET /api/condition/:jia_isu_uuid
// ISUのコンディションを取得
func getIsuConditions(c echo.Context) error {
	jiaUserID, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	jiaIsuUUID := c.Param("jia_isu_uuid")
	if jiaIsuUUID == "" {
		return c.String(http.StatusBadRequest, "missing: jia_isu_uuid")
	}

	endTimeInt64, err := strconv.ParseInt(c.QueryParam("end_time"), 10, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad format: end_time")
	}
	endTime := time.Unix(endTimeInt64, 0)
	conditionLevelCSV := c.QueryParam("condition_level")
	if conditionLevelCSV == "" {
		return c.String(http.StatusBadRequest, "missing: condition_level")
	}
	conditionLevel := map[string]interface{}{}
	for _, level := range strings.Split(conditionLevelCSV, ",") {
		conditionLevel[level] = struct{}{}
	}

	startTimeStr := c.QueryParam("start_time")
	var startTime time.Time
	if startTimeStr != "" {
		startTimeInt64, err := strconv.ParseInt(startTimeStr, 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "bad format: start_time")
		}
		startTime = time.Unix(startTimeInt64, 0)
	}

	var isuName string
	metadata, found, loaded := getCachedOwnedIsuMetadata(jiaUserID, jiaIsuUUID)
	if loaded {
		if !found {
			return c.String(http.StatusNotFound, "not found: isu")
		}
		isuName = metadata.Name
	} else {
		err = db.Get(&isuName,
			"SELECT name FROM `isu` WHERE `jia_isu_uuid` = ? AND `jia_user_id` = ?",
			jiaIsuUUID, jiaUserID,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.String(http.StatusNotFound, "not found: isu")
			}

			c.Logger().Errorf("db error: %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	conditionsResponse, loaded, err := getIsuConditionsFromCache(jiaIsuUUID, endTime, conditionLevel, startTime, conditionLimit, isuName)
	if err == nil && !loaded {
		conditionsResponse, err = getIsuConditionsFromDB(db, jiaIsuUUID, endTime, conditionLevel, startTime, conditionLimit, isuName)
	}
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, conditionsResponse)
}

func getIsuConditionsFromCache(jiaIsuUUID string, endTime time.Time, conditionLevel map[string]interface{}, startTime time.Time,
	limit int, isuName string) ([]*GetIsuConditionResponse, bool, error) {
	conditionHistoryCache.RLock()
	loaded := conditionHistoryCache.loaded
	history := conditionHistoryCache.histories[jiaIsuUUID]
	conditionHistoryCache.RUnlock()
	if !loaded {
		return nil, false, nil
	}
	if history == nil {
		return []*GetIsuConditionResponse{}, true, nil
	}

	allowedList := conditionStringsForLevels(conditionLevel)
	allowed := make(map[string]struct{}, len(allowedList))
	for _, condition := range allowedList {
		allowed[condition] = struct{}{}
	}
	if len(allowed) == 0 {
		return []*GetIsuConditionResponse{}, true, nil
	}

	response := make([]*GetIsuConditionResponse, 0, limit)
	endUnix := endTime.Unix()
	startUnix := startTime.Unix()
	history.RLock()
	defer history.RUnlock()
	for i := len(history.conditions) - 1; i >= 0 && len(response) < limit; i-- {
		condition := history.conditions[i]
		if condition.Timestamp >= endUnix {
			continue
		}
		if !startTime.IsZero() && condition.Timestamp < startUnix {
			break
		}
		if _, ok := allowed[condition.Condition]; !ok {
			continue
		}
		level, err := calculateConditionLevel(condition.Condition)
		if err != nil {
			continue
		}
		response = append(response, &GetIsuConditionResponse{
			JIAIsuUUID:     jiaIsuUUID,
			IsuName:        isuName,
			Timestamp:      condition.Timestamp,
			IsSitting:      condition.IsSitting,
			Condition:      condition.Condition,
			ConditionLevel: level,
			Message:        condition.Message,
		})
	}
	return response, true, nil
}

// ISUのコンディションをDBから取得
func getIsuConditionsFromDB(db *sqlx.DB, jiaIsuUUID string, endTime time.Time, conditionLevel map[string]interface{}, startTime time.Time,
	limit int, isuName string) ([]*GetIsuConditionResponse, error) {

	allowedConditions := conditionStringsForLevels(conditionLevel)
	if len(allowedConditions) == 0 {
		return []*GetIsuConditionResponse{}, nil
	}

	conditions := []IsuCondition{}
	query := "SELECT `jia_isu_uuid`, `timestamp`, `is_sitting`, `condition`, `message`" +
		" FROM `isu_condition` WHERE `jia_isu_uuid` = ? AND `timestamp` < ?"
	args := []interface{}{jiaIsuUUID, endTime}
	if !startTime.IsZero() {
		query += " AND `timestamp` >= ?"
		args = append(args, startTime)
	}
	query += " AND `condition` IN (" + strings.TrimSuffix(strings.Repeat("?,", len(allowedConditions)), ",") + ")"
	for _, condition := range allowedConditions {
		args = append(args, condition)
	}
	query += " ORDER BY `timestamp` DESC LIMIT ?"
	args = append(args, limit)

	err := db.Select(&conditions, query, args...)
	if err != nil {
		return nil, fmt.Errorf("db error: %v", err)
	}

	conditionsResponse := []*GetIsuConditionResponse{}
	for _, c := range conditions {
		cLevel, err := calculateConditionLevel(c.Condition)
		if err != nil {
			continue
		}

		data := GetIsuConditionResponse{
			JIAIsuUUID:     c.JIAIsuUUID,
			IsuName:        isuName,
			Timestamp:      c.Timestamp.Unix(),
			IsSitting:      c.IsSitting,
			Condition:      c.Condition,
			ConditionLevel: cLevel,
			Message:        c.Message,
		}
		conditionsResponse = append(conditionsResponse, &data)
	}

	return conditionsResponse, nil
}

func conditionStringsForLevels(levels map[string]interface{}) []string {
	conditions := []string{}
	for bits := 0; bits < 8; bits++ {
		condition := fmt.Sprintf(
			"is_dirty=%t,is_overweight=%t,is_broken=%t",
			bits&1 != 0, bits&2 != 0, bits&4 != 0,
		)
		level, err := calculateConditionLevel(condition)
		if err != nil {
			continue
		}
		if _, ok := levels[level]; ok {
			conditions = append(conditions, condition)
		}
	}
	return conditions
}

// ISUのコンディションの文字列からコンディションレベルを計算
func calculateConditionLevel(condition string) (string, error) {
	var conditionLevel string

	warnCount := strings.Count(condition, "=true")
	switch warnCount {
	case 0:
		conditionLevel = conditionLevelInfo
	case 1, 2:
		conditionLevel = conditionLevelWarning
	case 3:
		conditionLevel = conditionLevelCritical
	default:
		return "", fmt.Errorf("unexpected warn count")
	}

	return conditionLevel, nil
}

// GET /api/trend
// ISUの性格毎の最新のコンディション情報
func buildTrendResponse() ([]TrendResponse, error) {
	latestConditions, characters, loaded := snapshotIsuMetadata()
	characterList := make([]Isu, 0, len(characters))
	if loaded {
		for _, character := range characters {
			characterList = append(characterList, Isu{Character: character})
		}
	} else {
		characterList = []Isu{}
		if err := db.Select(&characterList, "SELECT `character` FROM `isu` GROUP BY `character`"); err != nil {
			return nil, fmt.Errorf("select characters: %w", err)
		}

		latestConditions = []TrendQueryRow{}
		if err := db.Select(&latestConditions,
			"SELECT `id`, `jia_isu_uuid`, `character` FROM `isu`"); err != nil {
			return nil, fmt.Errorf("select latest conditions: %w", err)
		}
	}

	type groupedConditions struct {
		info     []*TrendCondition
		warning  []*TrendCondition
		critical []*TrendCondition
	}
	newGroupedConditions := func() *groupedConditions {
		return &groupedConditions{
			info:     []*TrendCondition{},
			warning:  []*TrendCondition{},
			critical: []*TrendCondition{},
		}
	}
	grouped := map[string]*groupedConditions{}
	latestByUUID := snapshotLatestConditions()
	for _, row := range latestConditions {
		if _, ok := grouped[row.Character]; !ok {
			grouped[row.Character] = newGroupedConditions()
		}
		latestCondition, ok := latestByUUID[row.JIAIsuUUID]
		if !ok {
			continue
		}
		conditionLevel, err := calculateConditionLevel(latestCondition.Condition)
		if err != nil {
			return nil, err
		}
		trendCondition := &TrendCondition{ID: row.ID, Timestamp: latestCondition.Timestamp}
		switch conditionLevel {
		case conditionLevelInfo:
			grouped[row.Character].info = append(grouped[row.Character].info, trendCondition)
		case conditionLevelWarning:
			grouped[row.Character].warning = append(grouped[row.Character].warning, trendCondition)
		case conditionLevelCritical:
			grouped[row.Character].critical = append(grouped[row.Character].critical, trendCondition)
		}
	}

	res := []TrendResponse{}
	for _, character := range characterList {
		conditions := grouped[character.Character]
		if conditions == nil {
			conditions = newGroupedConditions()
		}

		sort.Slice(conditions.info, func(i, j int) bool {
			return conditions.info[i].Timestamp > conditions.info[j].Timestamp
		})
		sort.Slice(conditions.warning, func(i, j int) bool {
			return conditions.warning[i].Timestamp > conditions.warning[j].Timestamp
		})
		sort.Slice(conditions.critical, func(i, j int) bool {
			return conditions.critical[i].Timestamp > conditions.critical[j].Timestamp
		})
		res = append(res,
			TrendResponse{
				Character: character.Character,
				Info:      conditions.info,
				Warning:   conditions.warning,
				Critical:  conditions.critical,
			})
	}

	return res, nil
}

func getTrend(c echo.Context) error {
	now := time.Now()
	trendCache.Lock()
	if len(trendCache.body) != 0 && now.Before(trendCache.expiresAt) {
		body := trendCache.body
		trendCache.Unlock()
		return c.Blob(http.StatusOK, echo.MIMEApplicationJSONCharsetUTF8, body)
	}

	res, err := buildTrendResponse()
	if err != nil {
		trendCache.Unlock()
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	body, err := json.Marshal(res)
	if err != nil {
		trendCache.Unlock()
		c.Logger().Errorf("json error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	trendCache.body = body
	trendCache.expiresAt = time.Now().Add(trendCacheTTL)
	trendCache.Unlock()

	return c.Blob(http.StatusOK, echo.MIMEApplicationJSONCharsetUTF8, body)
}

// POST /api/condition/:jia_isu_uuid
// ISUからのコンディションを受け取る
func postIsuCondition(c echo.Context) error {
	jiaIsuUUID := c.Param("jia_isu_uuid")
	if jiaIsuUUID == "" {
		return c.String(http.StatusBadRequest, "missing: jia_isu_uuid")
	}

	body, err := ioutil.ReadAll(c.Request().Body)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad request body")
	}
	req := make([]CachedCondition, 0, bytes.Count(body, []byte("{")))
	err = conditionJSON.Unmarshal(body, &req)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad request body")
	} else if len(req) == 0 {
		return c.String(http.StatusBadRequest, "bad request body")
	}

	conditionWriteBarrier.RLock()
	defer conditionWriteBarrier.RUnlock()

	known, err := isKnownIsu(jiaIsuUUID)
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	if !known {
		return c.String(http.StatusNotFound, "not found: isu")
	}

	for i := range req {
		canonical, ok := canonicalConditionStrings[req[i].Condition]
		if !ok {
			return c.String(http.StatusBadRequest, "bad request body")
		}
		req[i].Condition = canonical
		req[i].Message = internConditionMessage(req[i].Message)
	}

	// During the one-minute scoring phase, the memory history is authoritative.
	// initialize recreates it from the baseline DB, so per-request remote SQL is
	// unnecessary and condition reads can observe the update immediately.
	cacheConditionHistory(jiaIsuUUID, req)

	return c.NoContent(http.StatusAccepted)
}

// ISUのコンディションの文字列がcsv形式になっているか検証
func isValidConditionFormat(conditionStr string) bool {

	keys := []string{"is_dirty=", "is_overweight=", "is_broken="}
	const valueTrue = "true"
	const valueFalse = "false"

	idxCondStr := 0

	for idxKeys, key := range keys {
		if !strings.HasPrefix(conditionStr[idxCondStr:], key) {
			return false
		}
		idxCondStr += len(key)

		if strings.HasPrefix(conditionStr[idxCondStr:], valueTrue) {
			idxCondStr += len(valueTrue)
		} else if strings.HasPrefix(conditionStr[idxCondStr:], valueFalse) {
			idxCondStr += len(valueFalse)
		} else {
			return false
		}

		if idxKeys < (len(keys) - 1) {
			if conditionStr[idxCondStr] != ',' {
				return false
			}
			idxCondStr++
		}
	}

	return (idxCondStr == len(conditionStr))
}

func getIndex(c echo.Context) error {
	return c.File(frontendContentsPath + "/index.html")
}
