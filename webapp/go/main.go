package main

import (
	"bytes"
	"crypto/ecdsa"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	conditionFlagSitting        = uint8(1 << 3)
	conditionRequestBufferSize  = 2048
	conditionForwardBatchLimit  = 64
	forwardBatchRequestBufSize  = 128 * 1024
)

var (
	db                  *sqlx.DB
	sessionStore        sessions.Store
	mySQLConnectionData *MySQLConnectionEnv
	conditionJSON       = jsoniter.ConfigCompatibleWithStandardLibrary

	jiaJWTSigningKey *ecdsa.PublicKey

	postIsuConditionTargetBaseURL string // JIAへのactivate時に登録する，ISUがconditionを送る先のURL
	registrationOnly              bool
	registrationGateURL           string
	registrationGateToken         string
	conditionForwardURL           string
	conditionForwardClient        *http.Client
	conditionForwardQueue         chan *conditionForwardRequest

	sessionCache = struct {
		sync.RWMutex
		users map[string]string
	}{users: make(map[string]string)}

	iconCache = struct {
		sync.RWMutex
		images map[string][]byte
	}{images: make(map[string][]byte)}

	knownIsuCache = struct {
		sync.RWMutex
		uuids  map[string]struct{}
		loaded bool
	}{uuids: make(map[string]struct{})}

	isuRegistrationLocks sync.Map
	knownIsuLookupLocks  [64]sync.Mutex
	isuRegistry          atomic.Value // *IsuRegistry; replaced after initialize

	registrationRequests = newRegistrationRequestGate()

	conditionState atomic.Value // *ConditionState; swapped as one initialize generation

	conditionBitsByString = map[string]uint8{
		"is_dirty=false,is_overweight=false,is_broken=false": 0,
		"is_dirty=true,is_overweight=false,is_broken=false":  1,
		"is_dirty=false,is_overweight=true,is_broken=false":  2,
		"is_dirty=true,is_overweight=true,is_broken=false":   3,
		"is_dirty=false,is_overweight=false,is_broken=true":  4,
		"is_dirty=true,is_overweight=false,is_broken=true":   5,
		"is_dirty=false,is_overweight=true,is_broken=true":   6,
		"is_dirty=true,is_overweight=true,is_broken=true":    7,
	}
	conditionStringByBits = [8]string{
		"is_dirty=false,is_overweight=false,is_broken=false",
		"is_dirty=true,is_overweight=false,is_broken=false",
		"is_dirty=false,is_overweight=true,is_broken=false",
		"is_dirty=true,is_overweight=true,is_broken=false",
		"is_dirty=false,is_overweight=false,is_broken=true",
		"is_dirty=true,is_overweight=false,is_broken=true",
		"is_dirty=false,is_overweight=true,is_broken=true",
		"is_dirty=true,is_overweight=true,is_broken=true",
	}
	conditionLevelByBits = [8]string{
		conditionLevelInfo,
		conditionLevelWarning,
		conditionLevelWarning,
		conditionLevelWarning,
		conditionLevelWarning,
		conditionLevelWarning,
		conditionLevelWarning,
		conditionLevelCritical,
	}

	conditionWriteBarrier sync.RWMutex
	conditionRequestPool  = sync.Pool{New: func() interface{} {
		return new(conditionRequestBuffer)
	}}
	conditionForwardRequestPool = sync.Pool{New: func() interface{} {
		return &conditionForwardRequest{result: make(chan int, 1)}
	}}
	forwardBatchRequestPool = sync.Pool{New: func() interface{} {
		return new(forwardBatchRequestBuffer)
	}}
)

type conditionRequestBuffer [conditionRequestBufferSize]byte
type forwardBatchRequestBuffer [forwardBatchRequestBufSize]byte

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

type ConditionState struct {
	sync.RWMutex
	histories map[string]*ConditionHistory
	loaded    bool

	messageMu  sync.RWMutex
	messages   []string
	messageIDs map[string]uint32

	trendMu        sync.Mutex
	trendBody      []byte
	trendExpiresAt time.Time
}

type CachedCondition struct {
	Timestamp int64
	MessageID uint32
	Flags     uint8
}

type registrationRequestGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	closed bool
	active int
}

type ForwardedCondition struct {
	Timestamp int64
	Message   string
	Flags     uint8
}

type conditionForwardRequest struct {
	jiaIsuUUID string
	conditions []ForwardedCondition
	result     chan int
}

func acquireConditionForwardRequest(jiaIsuUUID string, conditions []ForwardedCondition) *conditionForwardRequest {
	request := conditionForwardRequestPool.Get().(*conditionForwardRequest)
	request.jiaIsuUUID = jiaIsuUUID
	request.conditions = conditions
	return request
}

func releaseConditionForwardRequest(request *conditionForwardRequest) {
	request.jiaIsuUUID = ""
	request.conditions = nil
	conditionForwardRequestPool.Put(request)
}

func newRegistrationRequestGate() *registrationRequestGate {
	gate := &registrationRequestGate{}
	gate.cond = sync.NewCond(&gate.mu)
	return gate
}

func (gate *registrationRequestGate) enter() {
	gate.mu.Lock()
	for gate.closed {
		gate.cond.Wait()
	}
	gate.active++
	gate.mu.Unlock()
}

func (gate *registrationRequestGate) leave() {
	gate.mu.Lock()
	gate.active--
	if gate.active == 0 {
		gate.cond.Broadcast()
	}
	gate.mu.Unlock()
}

func (gate *registrationRequestGate) closeAndDrain() {
	gate.mu.Lock()
	gate.closed = true
	for gate.active != 0 {
		gate.cond.Wait()
	}
	gate.mu.Unlock()
}

func (gate *registrationRequestGate) open() {
	gate.mu.Lock()
	gate.closed = false
	gate.cond.Broadcast()
	gate.mu.Unlock()
}

type IncomingCondition struct {
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

// IsuRegistry is the read-side view of the durable ISU metadata. MariaDB
// remains authoritative, but public reads use this generation-local index
// instead of issuing the same small SELECT tens of thousands of times.
type IsuRegistry struct {
	sync.RWMutex
	byUser         map[string][]IsuListQueryRow
	byUserUUID     map[string]Isu
	trendRows      []TrendQueryRow
	characters     []string
	characterKnown map[string]struct{}
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
	registrationOnly = getEnv("REGISTRATION_ONLY", "") == "1"
	registrationGateURL = os.Getenv("REGISTRATION_GATE_URL")
	registrationGateToken = os.Getenv("REGISTRATION_GATE_TOKEN")
	conditionForwardURL = os.Getenv("CONDITION_FORWARD_URL")
	conditionForwardClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        512,
			MaxIdleConnsPerHost: 512,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: 5 * time.Second,
	}
	if registrationOnly && conditionForwardURL != "" {
		startConditionForwarders(8)
	}

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
	e.POST("/internal/registration-gate/close", postRegistrationGateClose)
	e.POST("/internal/registration-gate/open", postRegistrationGateOpen)
	e.POST("/internal/condition", postForwardedCondition)
	e.POST("/internal/condition-batch", postForwardedConditionBatch)

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
	maxDBConnections := 64
	if registrationOnly {
		maxDBConnections = 24
	}
	db.SetMaxOpenConns(maxDBConnections)
	db.SetMaxIdleConns(maxDBConnections)
	if registrationOnly {
		conditionState.Store(newConditionState())
	} else {
		if err = reloadConditionHistories(); err != nil {
			e.Logger.Fatalf("failed to load condition histories: %v", err)
			return
		}
		if err = reloadIsuRegistry(); err != nil {
			e.Logger.Fatalf("failed to load ISU registry: %v", err)
			return
		}
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
	state := currentConditionState()
	if state == nil {
		return
	}
	state.trendMu.Lock()
	state.trendBody = nil
	state.trendExpiresAt = time.Time{}
	state.trendMu.Unlock()
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
	var uuids []string
	if err := db.Select(&uuids, "SELECT `jia_isu_uuid` FROM `isu`"); err != nil {
		return err
	}

	known := make(map[string]struct{}, len(uuids))
	for _, uuid := range uuids {
		known[uuid] = struct{}{}
	}

	knownIsuCache.Lock()
	knownIsuCache.uuids = known
	knownIsuCache.loaded = true
	knownIsuCache.Unlock()
	return nil
}

func buildIsuRegistry(isus []Isu) *IsuRegistry {
	registry := &IsuRegistry{
		byUser:         make(map[string][]IsuListQueryRow),
		byUserUUID:     make(map[string]Isu, len(isus)),
		trendRows:      make([]TrendQueryRow, 0, len(isus)),
		characters:     make([]string, 0),
		characterKnown: make(map[string]struct{}),
	}
	for _, isu := range isus {
		listRow := IsuListQueryRow{
			ID:         isu.ID,
			JIAIsuUUID: isu.JIAIsuUUID,
			Name:       isu.Name,
			Character:  isu.Character,
		}
		registry.byUser[isu.JIAUserID] = append(registry.byUser[isu.JIAUserID], listRow)
		registry.byUserUUID[iconCacheKey(isu.JIAUserID, isu.JIAIsuUUID)] = isu
		registry.trendRows = append(registry.trendRows, TrendQueryRow{
			ID:         isu.ID,
			JIAIsuUUID: isu.JIAIsuUUID,
			Character:  isu.Character,
		})
		if _, ok := registry.characterKnown[isu.Character]; !ok {
			registry.characterKnown[isu.Character] = struct{}{}
			registry.characters = append(registry.characters, isu.Character)
		}
	}
	for userID := range registry.byUser {
		sort.Slice(registry.byUser[userID], func(i, j int) bool {
			return registry.byUser[userID][i].ID > registry.byUser[userID][j].ID
		})
	}
	sort.Strings(registry.characters)
	return registry
}

func reloadIsuRegistry() error {
	isus := []Isu{}
	if err := db.Select(&isus,
		"SELECT `id`, `jia_isu_uuid`, `name`, `character`, `jia_user_id` FROM `isu`"); err != nil {
		return err
	}
	registry := buildIsuRegistry(isus)
	isuRegistry.Store(registry)

	known := make(map[string]struct{}, len(isus))
	for _, isu := range isus {
		known[isu.JIAIsuUUID] = struct{}{}
	}
	knownIsuCache.Lock()
	knownIsuCache.uuids = known
	knownIsuCache.loaded = true
	knownIsuCache.Unlock()
	return nil
}

func currentIsuRegistry() *IsuRegistry {
	value := isuRegistry.Load()
	if value == nil {
		return nil
	}
	return value.(*IsuRegistry)
}

func (registry *IsuRegistry) listForUser(jiaUserID string) []IsuListQueryRow {
	registry.RLock()
	defer registry.RUnlock()
	return append([]IsuListQueryRow(nil), registry.byUser[jiaUserID]...)
}

func (registry *IsuRegistry) get(jiaUserID string, jiaIsuUUID string) (Isu, bool) {
	registry.RLock()
	defer registry.RUnlock()
	isu, ok := registry.byUserUUID[iconCacheKey(jiaUserID, jiaIsuUUID)]
	return isu, ok
}

func (registry *IsuRegistry) trendSnapshot() ([]string, []TrendQueryRow) {
	registry.RLock()
	defer registry.RUnlock()
	return append([]string(nil), registry.characters...), append([]TrendQueryRow(nil), registry.trendRows...)
}

func (registry *IsuRegistry) add(isu Isu) {
	registry.Lock()
	defer registry.Unlock()
	listRow := IsuListQueryRow{
		ID:         isu.ID,
		JIAIsuUUID: isu.JIAIsuUUID,
		Name:       isu.Name,
		Character:  isu.Character,
	}
	registry.byUser[isu.JIAUserID] = append([]IsuListQueryRow{listRow}, registry.byUser[isu.JIAUserID]...)
	registry.byUserUUID[iconCacheKey(isu.JIAUserID, isu.JIAIsuUUID)] = isu
	registry.trendRows = append(registry.trendRows, TrendQueryRow{
		ID:         isu.ID,
		JIAIsuUUID: isu.JIAIsuUUID,
		Character:  isu.Character,
	})
	if _, ok := registry.characterKnown[isu.Character]; !ok {
		registry.characterKnown[isu.Character] = struct{}{}
		registry.characters = append(registry.characters, isu.Character)
		sort.Strings(registry.characters)
	}
}

func cacheKnownIsu(jiaIsuUUID string) {
	knownIsuCache.Lock()
	knownIsuCache.uuids[jiaIsuUUID] = struct{}{}
	knownIsuCache.Unlock()
}

func knownIsuLookupLock(jiaIsuUUID string) *sync.Mutex {
	const offset32 = uint32(2166136261)
	const prime32 = uint32(16777619)
	hash := offset32
	for index := 0; index < len(jiaIsuUUID); index++ {
		hash ^= uint32(jiaIsuUUID[index])
		hash *= prime32
	}
	return &knownIsuLookupLocks[hash%uint32(len(knownIsuLookupLocks))]
}

func isKnownIsu(jiaIsuUUID string) (bool, error) {
	knownIsuCache.RLock()
	_, ok := knownIsuCache.uuids[jiaIsuUUID]
	knownIsuCache.RUnlock()
	if ok {
		return true, nil
	}

	// Registrations may be handled by another process. Serialize misses in a
	// bounded set of stripes, then recheck before consulting the shared DB.
	lookupLock := knownIsuLookupLock(jiaIsuUUID)
	lookupLock.Lock()
	defer lookupLock.Unlock()
	knownIsuCache.RLock()
	_, ok = knownIsuCache.uuids[jiaIsuUUID]
	knownIsuCache.RUnlock()
	if ok {
		return true, nil
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM `isu` WHERE `jia_isu_uuid` = ?", jiaIsuUUID); err != nil {
		return false, err
	}
	if count == 0 {
		// Do not cache misses: JIA starts sending conditions before the
		// registration transaction is committed, so a retry must recheck.
		return false, nil
	}
	cacheKnownIsu(jiaIsuUUID)
	return true, nil
}

func newConditionState() *ConditionState {
	return &ConditionState{
		histories:  make(map[string]*ConditionHistory),
		messages:   []string{""},
		messageIDs: map[string]uint32{"": 0},
	}
}

func currentConditionState() *ConditionState {
	value := conditionState.Load()
	if value == nil {
		return nil
	}
	return value.(*ConditionState)
}

func internConditionMessage(state *ConditionState, message string) uint32 {
	state.messageMu.RLock()
	id, ok := state.messageIDs[message]
	state.messageMu.RUnlock()
	if ok {
		return id
	}

	state.messageMu.Lock()
	defer state.messageMu.Unlock()
	if id, ok = state.messageIDs[message]; ok {
		return id
	}
	id = uint32(len(state.messages))
	state.messages = append(state.messages, message)
	state.messageIDs[message] = id
	return id
}

func conditionMessage(state *ConditionState, id uint32) string {
	state.messageMu.RLock()
	defer state.messageMu.RUnlock()
	if int(id) >= len(state.messages) {
		return ""
	}
	return state.messages[id]
}

func cachedConditionBits(condition CachedCondition) uint8 {
	return condition.Flags & 0x07
}

func cachedConditionString(condition CachedCondition) string {
	return conditionStringByBits[cachedConditionBits(condition)]
}

func cachedConditionLevel(condition CachedCondition) string {
	return conditionLevelByBits[cachedConditionBits(condition)]
}

func cachedConditionIsSitting(condition CachedCondition) bool {
	return condition.Flags&conditionFlagSitting != 0
}

func latestCachedCondition(state *ConditionState, jiaIsuUUID string) (CachedCondition, bool) {
	if state == nil {
		return CachedCondition{}, false
	}
	state.RLock()
	history := state.histories[jiaIsuUUID]
	state.RUnlock()
	if history == nil {
		return CachedCondition{}, false
	}
	history.RLock()
	defer history.RUnlock()
	if len(history.conditions) == 0 {
		return CachedCondition{}, false
	}
	return history.conditions[len(history.conditions)-1], true
}

func reloadConditionHistories() error {
	state := newConditionState()
	rows, err := db.Queryx(
		"SELECT `jia_isu_uuid`, `timestamp`, `is_sitting`, `condition`, `message`" +
			" FROM `isu_condition` ORDER BY `jia_isu_uuid`, `timestamp`, `id`")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var condition IsuCondition
		if err = rows.StructScan(&condition); err != nil {
			return err
		}
		history := state.histories[condition.JIAIsuUUID]
		if history == nil {
			history = &ConditionHistory{}
			state.histories[condition.JIAIsuUUID] = history
		}
		bits, ok := conditionBitsByString[condition.Condition]
		if !ok {
			return fmt.Errorf("invalid condition in baseline: %q", condition.Condition)
		}
		flags := bits
		if condition.IsSitting {
			flags |= conditionFlagSitting
		}
		history.conditions = append(history.conditions, CachedCondition{
			Timestamp: condition.Timestamp.Unix(),
			MessageID: internConditionMessage(state, condition.Message),
			Flags:     flags,
		})
	}
	if err = rows.Err(); err != nil {
		return err
	}

	state.loaded = true
	conditionState.Store(state)
	return nil
}

func getOrCreateConditionHistory(state *ConditionState, jiaIsuUUID string) *ConditionHistory {
	state.RLock()
	history := state.histories[jiaIsuUUID]
	state.RUnlock()
	if history != nil {
		return history
	}

	state.Lock()
	defer state.Unlock()
	history = state.histories[jiaIsuUUID]
	if history == nil {
		history = &ConditionHistory{}
		state.histories[jiaIsuUUID] = history
	}
	return history
}

func cacheConditionHistory(state *ConditionState, jiaIsuUUID string, conditions []CachedCondition) {
	history := getOrCreateConditionHistory(state, jiaIsuUUID)
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

func conditionHistoryRange(state *ConditionState, jiaIsuUUID string, startTime, endTime time.Time) ([]CachedCondition, bool) {
	if state == nil {
		return nil, false
	}
	state.RLock()
	loaded := state.loaded
	history := state.histories[jiaIsuUUID]
	state.RUnlock()
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
	if !registrationOnly && cookieErr == nil {
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

	if !registrationOnly && cookieErr == nil {
		sessionCache.Lock()
		sessionCache.users[cookie.Value] = jiaUserID
		sessionCache.Unlock()
	}

	return jiaUserID, 0, nil
}

func postRegistrationGateClose(c echo.Context) error {
	if !registrationOnly || registrationGateToken == "" || c.Request().Header.Get("X-Registration-Gate-Token") != registrationGateToken {
		return c.NoContent(http.StatusNotFound)
	}
	registrationRequests.closeAndDrain()
	return c.NoContent(http.StatusNoContent)
}

func postRegistrationGateOpen(c echo.Context) error {
	if !registrationOnly || registrationGateToken == "" || c.Request().Header.Get("X-Registration-Gate-Token") != registrationGateToken {
		return c.NoContent(http.StatusNotFound)
	}
	registrationRequests.open()
	return c.NoContent(http.StatusNoContent)
}

func setRemoteRegistrationGate(action string) error {
	if registrationGateURL == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(registrationGateURL, "/")+"/"+action, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Registration-Gate-Token", registrationGateToken)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("registration gate %s returned %s", action, res.Status)
	}
	return nil
}

func decodeIncomingConditions(body []byte) ([]ForwardedCondition, error) {
	iterator := conditionJSON.BorrowIterator(body)
	defer conditionJSON.ReturnIterator(iterator)
	conditions := make([]ForwardedCondition, 0, bytes.Count(body, []byte("{")))
	for iterator.ReadArray() {
		condition := ForwardedCondition{Flags: 0xff}
		isSitting := false
		iterator.ReadObjectCB(func(iterator *jsoniter.Iterator, field string) bool {
			switch field {
			case "timestamp":
				if iterator.ReadNil() {
					condition.Timestamp = 0
				} else {
					condition.Timestamp = iterator.ReadInt64()
				}
			case "condition":
				if iterator.ReadNil() {
					condition.Flags = 0xff
				} else if flags, ok := conditionBitsByString[iterator.ReadString()]; ok {
					condition.Flags = flags
				} else {
					// The authoritative process checks UUID existence before format,
					// so carry an invalid marker across the private hop.
					condition.Flags = 0xff
				}
			case "message":
				if iterator.ReadNil() {
					condition.Message = ""
				} else {
					condition.Message = iterator.ReadString()
				}
			case "is_sitting":
				if iterator.ReadNil() {
					isSitting = false
				} else {
					isSitting = iterator.ReadBool()
				}
			default:
				iterator.Skip()
			}
			return iterator.Error == nil
		})
		if iterator.Error != nil {
			return nil, fmt.Errorf("bad request body: %w", iterator.Error)
		}
		if condition.Flags != 0xff && isSitting {
			condition.Flags |= conditionFlagSitting
		}
		conditions = append(conditions, condition)
	}
	if iterator.Error != nil {
		return nil, fmt.Errorf("bad request body: %w", iterator.Error)
	}
	// ReadArray consumes the closing bracket but not trailing input. Match
	// json.Unmarshal by accepting only whitespace until EOF.
	if next := iterator.WhatIsNext(); next != jsoniter.InvalidValue || !errors.Is(iterator.Error, io.EOF) {
		return nil, fmt.Errorf("bad request body")
	}
	if len(conditions) == 0 {
		return nil, fmt.Errorf("bad request body")
	}
	return conditions, nil
}

func readConditionRequestBody(request *http.Request) ([]byte, *conditionRequestBuffer, error) {
	if request.ContentLength < 0 || request.ContentLength > conditionRequestBufferSize {
		body, err := ioutil.ReadAll(request.Body)
		return body, nil, err
	}

	buffer := conditionRequestPool.Get().(*conditionRequestBuffer)
	body := buffer[:int(request.ContentLength)]
	if _, err := io.ReadFull(request.Body, body); err != nil {
		conditionRequestPool.Put(buffer)
		return nil, nil, err
	}
	return body, buffer, nil
}

func releaseConditionRequestBuffer(buffer *conditionRequestBuffer) {
	if buffer != nil {
		conditionRequestPool.Put(buffer)
	}
}

func readForwardBatchRequestBody(request *http.Request) ([]byte, *forwardBatchRequestBuffer, error) {
	if request.ContentLength < 0 || request.ContentLength > forwardBatchRequestBufSize {
		body, err := ioutil.ReadAll(request.Body)
		return body, nil, err
	}

	buffer := forwardBatchRequestPool.Get().(*forwardBatchRequestBuffer)
	body := buffer[:int(request.ContentLength)]
	if _, err := io.ReadFull(request.Body, body); err != nil {
		forwardBatchRequestPool.Put(buffer)
		return nil, nil, err
	}
	return body, buffer, nil
}

func releaseForwardBatchRequestBuffer(buffer *forwardBatchRequestBuffer) {
	if buffer != nil {
		forwardBatchRequestPool.Put(buffer)
	}
}

func forwardedConditionsEncodedSize(jiaIsuUUID string, conditions []ForwardedCondition) (int, error) {
	if len(jiaIsuUUID) > int(^uint16(0)) || uint64(len(conditions)) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("condition batch is too large")
	}
	size := uint64(4 + 2 + len(jiaIsuUUID) + 4)
	for index := range conditions {
		if uint64(len(conditions[index].Message)) > uint64(^uint32(0)) {
			return 0, fmt.Errorf("condition message is too large")
		}
		size += 8 + 1 + 4 + uint64(len(conditions[index].Message))
	}
	if size > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("condition batch is too large")
	}
	return int(size), nil
}

func writeForwardedConditions(body []byte, offset int, jiaIsuUUID string, conditions []ForwardedCondition) int {
	copy(body[offset:], "ICD1")
	offset += 4
	binary.LittleEndian.PutUint16(body[offset:], uint16(len(jiaIsuUUID)))
	offset += 2
	copy(body[offset:], jiaIsuUUID)
	offset += len(jiaIsuUUID)
	binary.LittleEndian.PutUint32(body[offset:], uint32(len(conditions)))
	offset += 4
	for index := range conditions {
		binary.LittleEndian.PutUint64(body[offset:], uint64(conditions[index].Timestamp))
		offset += 8
		body[offset] = conditions[index].Flags
		offset++
		binary.LittleEndian.PutUint32(body[offset:], uint32(len(conditions[index].Message)))
		offset += 4
		copy(body[offset:], conditions[index].Message)
		offset += len(conditions[index].Message)
	}
	return offset
}

func encodeForwardedConditions(jiaIsuUUID string, conditions []ForwardedCondition) ([]byte, error) {
	size, err := forwardedConditionsEncodedSize(jiaIsuUUID, conditions)
	if err != nil {
		return nil, err
	}
	body := make([]byte, size)
	writeForwardedConditions(body, 0, jiaIsuUUID, conditions)
	return body, nil
}

func decodeForwardedConditions(body []byte) (string, []ForwardedCondition, error) {
	if len(body) < 10 || string(body[:4]) != "ICD1" {
		return "", nil, fmt.Errorf("invalid condition batch")
	}
	offset := 4
	uuidLength := int(binary.LittleEndian.Uint16(body[offset:]))
	offset += 2
	if uuidLength == 0 || uuidLength > len(body)-offset-4 {
		return "", nil, fmt.Errorf("invalid condition UUID")
	}
	jiaIsuUUID := string(body[offset : offset+uuidLength])
	offset += uuidLength
	conditionCount := uint64(binary.LittleEndian.Uint32(body[offset:]))
	offset += 4
	if conditionCount == 0 || conditionCount > uint64((len(body)-offset)/13) {
		return "", nil, fmt.Errorf("invalid condition count")
	}
	conditions := make([]ForwardedCondition, int(conditionCount))
	for index := range conditions {
		if len(body)-offset < 13 {
			return "", nil, fmt.Errorf("truncated condition")
		}
		conditions[index].Timestamp = int64(binary.LittleEndian.Uint64(body[offset:]))
		offset += 8
		conditions[index].Flags = body[offset]
		offset++
		messageLength := uint64(binary.LittleEndian.Uint32(body[offset:]))
		offset += 4
		if messageLength > uint64(len(body)-offset) {
			return "", nil, fmt.Errorf("truncated condition message")
		}
		conditions[index].Message = string(body[offset : offset+int(messageLength)])
		offset += int(messageLength)
	}
	if offset != len(body) {
		return "", nil, fmt.Errorf("trailing condition data")
	}
	return jiaIsuUUID, conditions, nil
}

func encodeForwardedConditionBatch(requests []*conditionForwardRequest) ([]byte, error) {
	if len(requests) == 0 || len(requests) > int(^uint16(0)) {
		return nil, fmt.Errorf("invalid forwarded batch size")
	}
	size := uint64(4 + 2)
	for index := range requests {
		payloadSize, err := forwardedConditionsEncodedSize(requests[index].jiaIsuUUID, requests[index].conditions)
		if err != nil {
			return nil, err
		}
		if uint64(payloadSize) > uint64(^uint32(0)) {
			return nil, fmt.Errorf("forwarded payload is too large")
		}
		size += 4 + uint64(payloadSize)
	}
	if size > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("forwarded batch is too large")
	}
	body := make([]byte, int(size))
	copy(body, "ICB1")
	binary.LittleEndian.PutUint16(body[4:], uint16(len(requests)))
	offset := 6
	for index := range requests {
		payloadSize, _ := forwardedConditionsEncodedSize(requests[index].jiaIsuUUID, requests[index].conditions)
		binary.LittleEndian.PutUint32(body[offset:], uint32(payloadSize))
		offset += 4
		offset = writeForwardedConditions(body, offset, requests[index].jiaIsuUUID, requests[index].conditions)
	}
	return body, nil
}

func decodeForwardedConditionBatch(body []byte) ([]string, [][]ForwardedCondition, error) {
	if len(body) < 6 || string(body[:4]) != "ICB1" {
		return nil, nil, fmt.Errorf("invalid forwarded batch")
	}
	count := int(binary.LittleEndian.Uint16(body[4:]))
	if count == 0 || count > (len(body)-6)/4 {
		return nil, nil, fmt.Errorf("invalid forwarded batch count")
	}
	uuids := make([]string, count)
	conditions := make([][]ForwardedCondition, count)
	offset := 6
	for index := 0; index < count; index++ {
		if len(body)-offset < 4 {
			return nil, nil, fmt.Errorf("truncated forwarded batch")
		}
		payloadLength := uint64(binary.LittleEndian.Uint32(body[offset:]))
		offset += 4
		if payloadLength > uint64(len(body)-offset) {
			return nil, nil, fmt.Errorf("truncated forwarded payload")
		}
		payloadEnd := offset + int(payloadLength)
		uuid, decoded, err := decodeForwardedConditions(body[offset:payloadEnd])
		if err != nil {
			return nil, nil, err
		}
		uuids[index] = uuid
		conditions[index] = decoded
		offset = payloadEnd
	}
	if offset != len(body) {
		return nil, nil, fmt.Errorf("trailing forwarded batch data")
	}
	return uuids, conditions, nil
}

func encodeForwardedConditionStatuses(statuses []int) ([]byte, error) {
	if len(statuses) == 0 || len(statuses) > int(^uint16(0)) {
		return nil, fmt.Errorf("invalid forwarded status count")
	}
	body := make([]byte, 6+2*len(statuses))
	copy(body, "ICR1")
	binary.LittleEndian.PutUint16(body[4:], uint16(len(statuses)))
	for index := range statuses {
		if statuses[index] < 0 || statuses[index] > int(^uint16(0)) {
			return nil, fmt.Errorf("invalid forwarded status")
		}
		binary.LittleEndian.PutUint16(body[6+2*index:], uint16(statuses[index]))
	}
	return body, nil
}

func decodeForwardedConditionStatuses(body []byte, expected int) ([]int, error) {
	statuses := make([]int, expected)
	if err := decodeForwardedConditionStatusesInto(body, statuses); err != nil {
		return nil, err
	}
	return statuses, nil
}

func decodeForwardedConditionStatusesInto(body []byte, statuses []int) error {
	if len(body) < 6 || string(body[:4]) != "ICR1" {
		return fmt.Errorf("invalid forwarded status response")
	}
	count := int(binary.LittleEndian.Uint16(body[4:]))
	if count != len(statuses) || len(body) != 6+2*count {
		return fmt.Errorf("unexpected forwarded status count")
	}
	for index := range statuses {
		statuses[index] = int(binary.LittleEndian.Uint16(body[6+2*index:]))
	}
	return nil
}

func applyForwardedConditions(jiaIsuUUID string, conditions []ForwardedCondition) (int, error) {
	conditionWriteBarrier.RLock()
	defer conditionWriteBarrier.RUnlock()
	state := currentConditionState()

	known, err := isKnownIsu(jiaIsuUUID)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if !known {
		return http.StatusNotFound, nil
	}
	for index := range conditions {
		if conditions[index].Flags > conditionFlagSitting|7 {
			return http.StatusBadRequest, nil
		}
	}

	compact := make([]CachedCondition, len(conditions))
	for index := range conditions {
		compact[index] = CachedCondition{
			Timestamp: conditions[index].Timestamp,
			MessageID: internConditionMessage(state, conditions[index].Message),
			Flags:     conditions[index].Flags,
		}
	}
	cacheConditionHistory(state, jiaIsuUUID, compact)
	return http.StatusAccepted, nil
}

func conditionStatusResponse(c echo.Context, status int) error {
	switch status {
	case http.StatusAccepted:
		return c.NoContent(status)
	case http.StatusBadRequest:
		return c.String(status, "bad request body")
	case http.StatusNotFound:
		return c.String(status, "not found: isu")
	default:
		return c.NoContent(http.StatusInternalServerError)
	}
}

func postForwardedCondition(c echo.Context) error {
	if registrationOnly || registrationGateToken == "" || c.Request().Header.Get("X-Registration-Gate-Token") != registrationGateToken {
		return c.NoContent(http.StatusNotFound)
	}
	body, err := ioutil.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	jiaIsuUUID, conditions, err := decodeForwardedConditions(body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	status, err := applyForwardedConditions(jiaIsuUUID, conditions)
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
	}
	return c.NoContent(status)
}

func postForwardedConditionBatch(c echo.Context) error {
	if registrationOnly || registrationGateToken == "" || c.Request().Header.Get("X-Registration-Gate-Token") != registrationGateToken {
		return c.NoContent(http.StatusNotFound)
	}
	body, pooledBody, err := readForwardBatchRequestBody(c.Request())
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	uuids, conditionBatches, err := decodeForwardedConditionBatch(body)
	releaseForwardBatchRequestBuffer(pooledBody)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	statuses := make([]int, len(uuids))
	for index := range uuids {
		statuses[index], err = applyForwardedConditions(uuids[index], conditionBatches[index])
		if err != nil {
			c.Logger().Errorf("db error: %v", err)
			statuses[index] = http.StatusInternalServerError
		}
	}
	response, err := encodeForwardedConditionStatuses(statuses)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.Blob(http.StatusOK, "application/octet-stream", response)
}

func startConditionForwarders(workerCount int) {
	conditionForwardQueue = make(chan *conditionForwardRequest, 65536)
	for worker := 0; worker < workerCount; worker++ {
		go conditionForwardWorker()
	}
}

func conditionForwardWorker() {
	for first := range conditionForwardQueue {
		requests := make([]*conditionForwardRequest, 1, conditionForwardBatchLimit)
		requests[0] = first
	collect:
		for len(requests) < cap(requests) {
			select {
			case request := <-conditionForwardQueue:
				requests = append(requests, request)
			default:
				break collect
			}
		}
		statuses := forwardConditionBatch(requests)
		for index := range requests {
			requests[index].result <- statuses[index]
		}
	}
}

func forwardConditionBatch(requests []*conditionForwardRequest) []int {
	statuses := make([]int, len(requests))
	for index := range statuses {
		statuses[index] = http.StatusInternalServerError
	}
	body, err := encodeForwardedConditionBatch(requests)
	if err != nil {
		return statuses
	}
	req, err := http.NewRequest(http.MethodPost, conditionForwardURL, bytes.NewReader(body))
	if err != nil {
		return statuses
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Registration-Gate-Token", registrationGateToken)
	res, err := conditionForwardClient.Do(req)
	if err != nil {
		return statuses
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return statuses
	}
	expectedResponseSize := 6 + 2*len(requests)
	if res.ContentLength >= 0 && res.ContentLength != int64(expectedResponseSize) {
		return statuses
	}
	var responseBuffer [6 + 2*conditionForwardBatchLimit + 1]byte
	responseBody := responseBuffer[:expectedResponseSize]
	if _, err = io.ReadFull(res.Body, responseBody); err != nil {
		return statuses
	}
	if count, readErr := res.Body.Read(responseBuffer[expectedResponseSize : expectedResponseSize+1]); count != 0 || readErr != io.EOF {
		return statuses
	}
	if err = decodeForwardedConditionStatusesInto(responseBody, statuses); err != nil {
		return statuses
	}
	return statuses
}

func postIsuConditionForward(c echo.Context, jiaIsuUUID string, conditions []ForwardedCondition) error {
	if conditionForwardQueue == nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	request := acquireConditionForwardRequest(jiaIsuUUID, conditions)
	conditionForwardQueue <- request
	status := <-request.result
	releaseConditionForwardRequest(request)
	return conditionStatusResponse(c, status)
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
	if err = setRemoteRegistrationGate("close"); err != nil {
		c.Logger().Errorf("failed to close registration gate: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	if registrationGateURL != "" {
		defer func() {
			if openErr := setRemoteRegistrationGate("open"); openErr != nil {
				c.Logger().Errorf("failed to open registration gate: %v", openErr)
			}
		}()
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

	if err = reloadIsuRegistry(); err != nil {
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

	registry := currentIsuRegistry()
	if registry == nil {
		c.Logger().Error("ISU registry is not loaded")
		return c.NoContent(http.StatusInternalServerError)
	}
	isuList := registry.listForUser(jiaUserID)

	responseList := []GetIsuListResponse{}
	state := currentConditionState()
	for _, isu := range isuList {
		var formattedCondition *GetIsuConditionResponse
		latestCondition, ok := latestCachedCondition(state, isu.JIAIsuUUID)
		if ok {
			formattedCondition = &GetIsuConditionResponse{
				JIAIsuUUID:     isu.JIAIsuUUID,
				IsuName:        isu.Name,
				Timestamp:      latestCondition.Timestamp,
				IsSitting:      cachedConditionIsSitting(latestCondition),
				Condition:      cachedConditionString(latestCondition),
				ConditionLevel: cachedConditionLevel(latestCondition),
				Message:        conditionMessage(state, latestCondition.MessageID),
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
	if registrationOnly {
		registrationRequests.enter()
		defer registrationRequests.leave()
	}

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

	registrationLockValue, _ := isuRegistrationLocks.LoadOrStore(jiaIsuUUID, &sync.Mutex{})
	registrationLock := registrationLockValue.(*sync.Mutex)
	registrationLock.Lock()
	defer registrationLock.Unlock()

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

	if !registrationOnly {
		isu.JIAUserID = jiaUserID
		registry := currentIsuRegistry()
		if registry == nil {
			c.Logger().Error("ISU registry is not loaded")
			return c.NoContent(http.StatusInternalServerError)
		}
		registry.add(isu)
		cacheKnownIsu(jiaIsuUUID)
		cacheIsuIcon(jiaUserID, jiaIsuUUID, image)
		invalidateTrendCache()
	}
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

	registry := currentIsuRegistry()
	if registry == nil {
		c.Logger().Error("ISU registry is not loaded")
		return c.NoContent(http.StatusInternalServerError)
	}
	res, ok := registry.get(jiaUserID, jiaIsuUUID)
	if !ok {
		return c.String(http.StatusNotFound, "not found: isu")
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

	registry := currentIsuRegistry()
	if registry == nil {
		c.Logger().Error("ISU registry is not loaded")
		return c.NoContent(http.StatusInternalServerError)
	}
	if _, ok := registry.get(jiaUserID, jiaIsuUUID); !ok {
		return c.String(http.StatusNotFound, "not found: isu")
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

	state := currentConditionState()
	conditions, loaded := conditionHistoryRange(state, jiaIsuUUID, graphDate, graphDate.Add(24*time.Hour))
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
			bits, ok := conditionBitsByString[condition.Condition]
			if !ok {
				return nil, fmt.Errorf("invalid condition: %q", condition.Condition)
			}
			flags := bits
			if condition.IsSitting {
				flags |= conditionFlagSitting
			}
			conditions = append(conditions, CachedCondition{
				Timestamp: condition.Timestamp.Unix(),
				Flags:     flags,
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
	isBrokenCount := 0
	isDirtyCount := 0
	isOverweightCount := 0
	rawScore := 0
	for _, condition := range isuConditions {
		bits := cachedConditionBits(condition)
		badConditionsCount := 0
		if bits&1 != 0 {
			isDirtyCount++
			badConditionsCount++
		}
		if bits&2 != 0 {
			isOverweightCount++
			badConditionsCount++
		}
		if bits&4 != 0 {
			isBrokenCount++
			badConditionsCount++
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
		if cachedConditionIsSitting(condition) {
			sittingCount++
		}
	}

	isuConditionsLength := len(isuConditions)

	score := rawScore * 100 / 3 / isuConditionsLength

	sittingPercentage := sittingCount * 100 / isuConditionsLength
	isBrokenPercentage := isBrokenCount * 100 / isuConditionsLength
	isOverweightPercentage := isOverweightCount * 100 / isuConditionsLength
	isDirtyPercentage := isDirtyCount * 100 / isuConditionsLength

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

	registry := currentIsuRegistry()
	if registry == nil {
		c.Logger().Error("ISU registry is not loaded")
		return c.NoContent(http.StatusInternalServerError)
	}
	isu, ok := registry.get(jiaUserID, jiaIsuUUID)
	if !ok {
		return c.String(http.StatusNotFound, "not found: isu")
	}
	isuName := isu.Name

	state := currentConditionState()
	conditionsResponse, loaded, err := getIsuConditionsFromCache(state, jiaIsuUUID, endTime, conditionLevel, startTime, conditionLimit, isuName)
	if err == nil && !loaded {
		conditionsResponse, err = getIsuConditionsFromDB(db, jiaIsuUUID, endTime, conditionLevel, startTime, conditionLimit, isuName)
	}
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, conditionsResponse)
}

func getIsuConditionsFromCache(state *ConditionState, jiaIsuUUID string, endTime time.Time, conditionLevel map[string]interface{}, startTime time.Time,
	limit int, isuName string) ([]*GetIsuConditionResponse, bool, error) {
	if state == nil {
		return nil, false, nil
	}
	state.RLock()
	loaded := state.loaded
	history := state.histories[jiaIsuUUID]
	state.RUnlock()
	if !loaded {
		return nil, false, nil
	}
	if history == nil {
		return []*GetIsuConditionResponse{}, true, nil
	}

	allowed := [8]bool{}
	allowedCount := 0
	for bits, level := range conditionLevelByBits {
		if _, ok := conditionLevel[level]; ok {
			allowed[bits] = true
			allowedCount++
		}
	}
	if allowedCount == 0 {
		return []*GetIsuConditionResponse{}, true, nil
	}

	selected := make([]CachedCondition, 0, limit)
	endUnix := endTime.Unix()
	startUnix := startTime.Unix()
	history.RLock()
	for i := len(history.conditions) - 1; i >= 0 && len(selected) < limit; i-- {
		condition := history.conditions[i]
		if condition.Timestamp >= endUnix {
			continue
		}
		if !startTime.IsZero() && condition.Timestamp < startUnix {
			break
		}
		if !allowed[cachedConditionBits(condition)] {
			continue
		}
		selected = append(selected, condition)
	}
	history.RUnlock()

	response := make([]*GetIsuConditionResponse, 0, len(selected))
	for _, condition := range selected {
		response = append(response, &GetIsuConditionResponse{
			JIAIsuUUID:     jiaIsuUUID,
			IsuName:        isuName,
			Timestamp:      condition.Timestamp,
			IsSitting:      cachedConditionIsSitting(condition),
			Condition:      cachedConditionString(condition),
			ConditionLevel: cachedConditionLevel(condition),
			Message:        conditionMessage(state, condition.MessageID),
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
func buildTrendResponse(state *ConditionState) ([]TrendResponse, error) {
	registry := currentIsuRegistry()
	if registry == nil {
		return nil, fmt.Errorf("ISU registry is not loaded")
	}
	registry.RLock()
	characterList := append([]string(nil), registry.characters...)

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
	state.RLock()
	for _, row := range registry.trendRows {
		if _, ok := grouped[row.Character]; !ok {
			grouped[row.Character] = newGroupedConditions()
		}
		history := state.histories[row.JIAIsuUUID]
		if history == nil {
			continue
		}
		history.RLock()
		if len(history.conditions) == 0 {
			history.RUnlock()
			continue
		}
		latestCondition := history.conditions[len(history.conditions)-1]
		history.RUnlock()
		trendCondition := &TrendCondition{ID: row.ID, Timestamp: latestCondition.Timestamp}
		switch cachedConditionLevel(latestCondition) {
		case conditionLevelInfo:
			grouped[row.Character].info = append(grouped[row.Character].info, trendCondition)
		case conditionLevelWarning:
			grouped[row.Character].warning = append(grouped[row.Character].warning, trendCondition)
		case conditionLevelCritical:
			grouped[row.Character].critical = append(grouped[row.Character].critical, trendCondition)
		}
	}
	state.RUnlock()
	registry.RUnlock()

	res := []TrendResponse{}
	for _, character := range characterList {
		conditions := grouped[character]
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
				Character: character,
				Info:      conditions.info,
				Warning:   conditions.warning,
				Critical:  conditions.critical,
			})
	}

	return res, nil
}

func getTrend(c echo.Context) error {
	now := time.Now()
	state := currentConditionState()
	state.trendMu.Lock()
	if len(state.trendBody) != 0 && now.Before(state.trendExpiresAt) {
		body := state.trendBody
		state.trendMu.Unlock()
		return c.Blob(http.StatusOK, echo.MIMEApplicationJSONCharsetUTF8, body)
	}

	res, err := buildTrendResponse(state)
	if err != nil {
		state.trendMu.Unlock()
		c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	body, err := json.Marshal(res)
	if err != nil {
		state.trendMu.Unlock()
		c.Logger().Errorf("json error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	state.trendBody = body
	state.trendExpiresAt = time.Now().Add(trendCacheTTL)
	state.trendMu.Unlock()

	return c.Blob(http.StatusOK, echo.MIMEApplicationJSONCharsetUTF8, body)
}

// POST /api/condition/:jia_isu_uuid
// ISUからのコンディションを受け取る
func postIsuCondition(c echo.Context) error {
	jiaIsuUUID := c.Param("jia_isu_uuid")
	if jiaIsuUUID == "" {
		return c.String(http.StatusBadRequest, "missing: jia_isu_uuid")
	}
	if registrationOnly {
		// Enter before reading the external body so initialize drains requests
		// that started in the previous generation but have not decoded yet.
		registrationRequests.enter()
		defer registrationRequests.leave()
	}

	body, pooledBody, err := readConditionRequestBody(c.Request())
	if err != nil {
		return c.String(http.StatusBadRequest, "bad request body")
	}
	conditions, err := decodeIncomingConditions(body)
	releaseConditionRequestBuffer(pooledBody)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad request body")
	}

	if registrationOnly {
		return postIsuConditionForward(c, jiaIsuUUID, conditions)
	}

	status, err := applyForwardedConditions(jiaIsuUUID, conditions)
	if err != nil {
		c.Logger().Errorf("db error: %v", err)
	}
	return conditionStatusResponse(c, status)
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
