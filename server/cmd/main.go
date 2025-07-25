package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/gofrs/uuid"
	"github.com/meshery/meshery/mesheryctl/pkg/constants"
	"github.com/meshery/meshery/server/handlers"
	"github.com/meshery/meshery/server/helpers"
	"github.com/meshery/meshery/server/helpers/utils"
	"github.com/meshery/meshery/server/internal/graphql"
	"github.com/meshery/meshery/server/internal/store"
	"github.com/meshery/meshery/server/machines"
	mhelpers "github.com/meshery/meshery/server/machines/helpers"
	"github.com/meshery/meshery/server/models"
	"github.com/meshery/meshery/server/models/connections"
	mesherymeshmodel "github.com/meshery/meshery/server/models/meshmodel"
	"github.com/meshery/meshery/server/router"
	"github.com/meshery/meshkit/broker/nats"
	"github.com/meshery/meshkit/logger"
	_events "github.com/meshery/meshkit/models/events"
	"github.com/meshery/meshkit/models/meshmodel/core/policies"
	meshmodel "github.com/meshery/meshkit/models/meshmodel/registry"
	"github.com/meshery/meshkit/utils/broadcast"
	"github.com/meshery/meshkit/utils/events"
	meshsyncmodel "github.com/meshery/meshsync/pkg/model"
	"github.com/spf13/viper"

	"github.com/meshery/schemas/models/v1beta1/environment"
	"github.com/meshery/schemas/models/v1beta1/workspace"
	"github.com/sirupsen/logrus"
)

var (
	globalTokenForAnonymousResults string
	version                        = "Not Set"
	commitsha                      = "Not Set"
	releasechannel                 = "Not Set"
)

const (
	// DefaultProviderURL is the provider url for the "none" provider
	DefaultProviderURL = "https://cloud.layer5.io"
	RelationshipsPath  = "../meshmodel/kubernetes/"
)

func main() {
	if globalTokenForAnonymousResults != "" {
		models.GlobalTokenForAnonymousResults = globalTokenForAnonymousResults
	}

	viper.AutomaticEnv()

	// Meshery Server configuration
	viper.SetConfigFile("./server-config.env")
	viper.WatchConfig()

	err := viper.ReadInConfig()
	if err != nil {
		logrus.Errorf("error reading config %v", err)
	}

	logLevel := viper.GetInt("LOG_LEVEL")
	if viper.GetBool("DEBUG") {
		logLevel = int(logrus.DebugLevel)
	}
	// Initialize Logger instance
	log, err := logger.New("meshery", logger.Options{
		Format:   logger.SyslogLogFormat,
		LogLevel: logLevel,
	})
	if err != nil {
		logrus.Error(err)
		os.Exit(1)
	}

	viper.OnConfigChange(func(event fsnotify.Event) {
		log.Info("received change for", event.Name)
		log.SetLevel(logrus.Level(viper.GetInt("LOG_LEVEL")))
	})

	instanceID, err := uuid.NewV4()
	if err != nil {
		log.Error(ErrCreatingUUIDInstance(err))
		os.Exit(1)
	}

	// operatingSystem, err := exec.Command("uname", "-s").Output()
	// if err != nil {
	// 	logrus.Error(err)
	// }

	ctx := context.Background()

	viper.AutomaticEnv()

	viper.SetDefault("PORT", 8080)
	viper.SetDefault("ADAPTER_URLS", "")
	viper.SetDefault("BUILD", version)
	viper.SetDefault("OS", "meshery")
	viper.SetDefault("COMMITSHA", commitsha)
	viper.SetDefault("RELEASE_CHANNEL", releasechannel)
	viper.SetDefault("INSTANCE_ID", &instanceID)
	viper.SetDefault(constants.ProviderENV, "")
	viper.SetDefault("REGISTER_STATIC_K8S", true)
	viper.SetDefault("SKIP_DOWNLOAD_CONTENT", false)
	viper.SetDefault("SKIP_COMP_GEN", false)
	viper.SetDefault("PLAYGROUND", false)
	viper.SetDefault("TMP_MESHSYNC_AS_A_LIBRARY_MODE", false)
	store.Initialize()

	log.Info("Local Provider capabilities are: ", version)

	// Get the channel
	log.Info("Meshery Server release channel is: ", releasechannel)

	home, err := os.UserHomeDir()
	if viper.GetString("USER_DATA_FOLDER") == "" {
		if err != nil {
			log.Error(ErrRetrievingUserHomeDirectory(err))
			os.Exit(1)
		}
		viper.SetDefault("USER_DATA_FOLDER", path.Join(home, ".meshery", "config"))
	}

	errDir := os.MkdirAll(viper.GetString("USER_DATA_FOLDER"), 0755)
	if errDir != nil {
		log.Error(ErrCreatingUserDataDirectory(viper.GetString("USER_DATA_FOLDER")))
		os.Exit(1)
	}
	logDir := path.Join(home, ".meshery", "logs", "registry")
	errDir = os.MkdirAll(logDir, 0755)
	if errDir != nil {
		logrus.Fatalf("Error creating user data directory: %v", err)
	}

	// Create or open the log file
	logFilePath := path.Join(logDir, "registry-logs.log")
	logFile, err := os.Create(logFilePath)
	if err != nil {
		logrus.Fatalf("Could not create log file: %v", err)
	}
	defer logFile.Close()
	viper.Set("REGISTRY_LOG_FILE", logFilePath)

	log.Info("Meshery Database is at: ", viper.GetString("USER_DATA_FOLDER"))
	if viper.GetString("KUBECONFIG_FOLDER") == "" {
		if err != nil {
			log.Error(ErrRetrievingUserHomeDirectory(err))
			os.Exit(1)
		}
		viper.SetDefault("KUBECONFIG_FOLDER", path.Join(home, ".kube"))
	}
	log.Info("Using kubeconfig at: ", viper.GetString("KUBECONFIG_FOLDER"))
	log.Info("Log level: ", log.GetLevel())

	adapterURLs := viper.GetStringSlice("ADAPTER_URLS")

	adapterTracker := helpers.NewAdaptersTracker(adapterURLs)
	queryTracker := helpers.NewUUIDQueryTracker()

	// Uncomment line below to generate a new UUID and force the user to login every time Meshery is started.
	// fileSessionStore := sessions.NewFilesystemStore("", []byte(uuid.NewV4().Bytes()))
	// fileSessionStore := sessions.NewFilesystemStore("", []byte("Meshery"))
	// fileSessionStore.MaxLength(0)

	provs := map[string]models.Provider{}

	preferencePersister, err := models.NewMapPreferencePersister()
	if err != nil {
		log.Error(ErrCreatingMapPreferencePersisterInstance(err))
		os.Exit(1)
	}
	defer preferencePersister.ClosePersister()

	dbHandler := models.GetNewDBInstance()
	regManager, err := meshmodel.NewRegistryManager(dbHandler)
	if err != nil {
		log.Error(ErrInitializingRegistryManager(err))
		os.Exit(1)
	}
	meshsyncCh := make(chan struct{}, 10)
	brokerConn := nats.NewEmptyConnection

	err = dbHandler.AutoMigrate(
		&meshsyncmodel.KubernetesKeyValue{},
		&meshsyncmodel.KubernetesResource{},
		&meshsyncmodel.KubernetesResourceSpec{},
		&meshsyncmodel.KubernetesResourceStatus{},
		&meshsyncmodel.KubernetesResourceObjectMeta{},
		&models.PerformanceProfile{},
		&models.MesheryResult{},
		&models.MesheryPattern{},
		&models.MesheryFilter{},
		&models.PatternResource{},
		&models.MesheryApplication{},
		&models.UserPreference{},
		&models.UserCapabilities{},
		&models.PerformanceTestConfig{},
		&models.SmiResultWithID{},
		models.K8sContext{},
		models.Organization{},
		models.Key{},
		connections.Connection{},
		environment.Environment{},
		environment.EnvironmentConnectionMapping{},
		workspace.Workspace{},
		workspace.WorkspacesEnvironmentsMapping{},
		workspace.WorkspacesDesignsMapping{},
		_events.Event{},
	)
	if err != nil {
		log.Error(ErrDatabaseAutoMigration(err))
		os.Exit(1)
	}

	lProv := &models.DefaultLocalProvider{
		ProviderBaseURL:                 DefaultProviderURL,
		MapPreferencePersister:          preferencePersister,
		UserCapabilitiesPersister:       &models.UserCapabilitiesPersister{DB: dbHandler},
		ResultPersister:                 &models.MesheryResultsPersister{DB: dbHandler},
		SmiResultPersister:              &models.SMIResultsPersister{DB: dbHandler},
		TestProfilesPersister:           &models.TestProfilesPersister{DB: dbHandler},
		PerformanceProfilesPersister:    &models.PerformanceProfilePersister{DB: dbHandler},
		MesheryPatternPersister:         &models.MesheryPatternPersister{DB: dbHandler},
		MesheryFilterPersister:          &models.MesheryFilterPersister{DB: dbHandler},
		MesheryApplicationPersister:     &models.MesheryApplicationPersister{DB: dbHandler},
		MesheryPatternResourcePersister: &models.PatternResourcePersister{DB: dbHandler},
		MesheryK8sContextPersister:      &models.MesheryK8sContextPersister{DB: dbHandler},
		OrganizationPersister:           &models.OrganizationPersister{DB: dbHandler},
		ConnectionPersister:             &models.ConnectionPersister{DB: dbHandler},
		EnvironmentPersister:            &models.EnvironmentPersister{DB: dbHandler},
		WorkspacePersister:              &models.WorkspacePersister{DB: dbHandler},
		KeyPersister:                    &models.KeyPersister{DB: dbHandler},
		EventsPersister:                 &models.EventsPersister{DB: dbHandler},
		GenericPersister:                dbHandler,
		Log:                             log,
	}

	// Local remote provider is initalized here.
	lProv.Initialize()

	hc := &models.HandlerConfig{
		Providers:              provs,
		ProviderCookieName:     "meshery-provider",
		ProviderCookieDuration: 30 * 24 * time.Hour,
		PlaygroundBuild:        viper.GetBool("PLAYGROUND"),
		AdapterTracker:         adapterTracker,
		QueryTracker:           queryTracker,

		KubeConfigFolder: viper.GetString("KUBECONFIG_FOLDER"),

		GrafanaClient:         models.NewGrafanaClient(&log),
		GrafanaClientForQuery: models.NewGrafanaClientWithHTTPClient(&http.Client{Timeout: time.Second}, &log),

		PrometheusClient:         models.NewPrometheusClient(&log),
		PrometheusClientForQuery: models.NewPrometheusClientWithHTTPClient(&http.Client{Timeout: time.Second}, &log),

		PatternChannel:            models.NewBroadcaster("Patterns"),
		FilterChannel:             models.NewBroadcaster("Filters"),
		EventBroadcaster:          models.NewBroadcaster("Events"),
		DashboardK8sResourcesChan: models.NewDashboardK8sResourcesHelper(),
		MeshModelSummaryChannel:   mesherymeshmodel.NewSummaryHelper(),

		K8scontextChannel: models.NewContextHelper(),
		OperatorTracker:   models.NewOperatorTracker(viper.GetBool("DISABLE_OPERATOR")),
	}
	krh, err := models.NewKeysRegistrationHelper(dbHandler, log)
	if err != nil {
		log.Error(ErrInitializingKeysRegistration(err))
		os.Exit(1)
	}
	//seed the local meshmodel components
	rego := policies.Rego{}

	go func() {
		// This is where models are seeded from meshmodel directory to registry
		models.SeedComponents(log, hc, regManager)
		// Rego is intialized for passing of policy if the policies are made to be per model base this needs to be removed.
		r, err := policies.NewRegoInstance(models.PoliciesPath, regManager)
		if err != nil {
			log.Warn(handlers.ErrCreatingOPAInstance(err))
		} else {
			rego = *r
		}
		krh.SeedKeys(viper.GetString("KEYS_PATH"))
		hc.MeshModelSummaryChannel.Publish()
	}()

	lProv.SeedContent(log)
	provs[lProv.Name()] = lProv

	providerEnvVar := viper.GetString(constants.ProviderENV)
	RemoteProviderURLs := viper.GetStringSlice("PROVIDER_BASE_URLS")
	for _, providerurl := range RemoteProviderURLs {
		parsedURL, err := url.Parse(providerurl)
		if err != nil {
			log.Error(ErrInvalidURLSkippingProvider(providerurl))
			continue
		}
		cp := &models.RemoteProvider{
			RemoteProviderURL:          parsedURL.String(),
			RefCookieName:              parsedURL.Host + "_ref",
			SessionName:                parsedURL.Host,
			TokenStore:                 make(map[string]string),
			LoginCookieDuration:        1 * time.Hour,
			SessionPreferencePersister: &models.SessionPreferencePersister{DB: dbHandler},
			UserCapabilitiesPersister:  &models.UserCapabilitiesPersister{DB: dbHandler},
			ProviderVersion:            version,
			SmiResultPersister:         &models.SMIResultsPersister{DB: dbHandler},
			GenericPersister:           dbHandler,
			EventsPersister:            &models.EventsPersister{DB: dbHandler},
			Log:                        log,
			CookieDuration:             24 * time.Hour,
		}

		cp.Initialize()

		cp.SyncPreferences()
		defer cp.StopSyncPreferences()
		provs[cp.Name()] = cp
	}

	// verifies if the provider specified in the "PROVIDER" environment variable is from one of the supported providers.
	// If it is one of the supported providers, the server gets configured to auto select the specified provider,
	// else the provider specified in the environment variable is ignored and  each time user logs in they need to select a provider.
	isProviderEnvVarValid := models.VerifyMesheryProvider(providerEnvVar, provs)
	if !isProviderEnvVarValid {
		providerEnvVar = ""
	}

	operatorDeploymentConfig := models.NewOperatorDeploymentConfig(adapterTracker)
	mctrlHelper := models.NewMesheryControllersHelper(log, operatorDeploymentConfig, dbHandler)
	connToInstanceTracker := machines.ConnectionToStateMachineInstanceTracker{
		ConnectToInstanceMap: make(map[uuid.UUID]*machines.StateMachine, 0),
	}

	k8sComponentsRegistrationHelper := models.NewComponentsRegistrationHelper(log)

	models.InitMeshSyncRegistrationQueue()
	mhelpers.InitRegistrationHelperSingleton(dbHandler, log, &connToInstanceTracker, hc.EventBroadcaster)
	policies.SyncRelationship.Lock()
	h := handlers.NewHandlerInstance(hc, meshsyncCh, log, brokerConn, k8sComponentsRegistrationHelper, mctrlHelper, dbHandler, events.NewEventStreamer(), regManager, providerEnvVar, &rego, &connToInstanceTracker)
	policies.SyncRelationship.Unlock()

	b := broadcast.NewBroadcaster(100)
	defer b.Close()

	g := graphql.New(graphql.Options{
		Config:      hc,
		Logger:      log,
		BrokerConn:  brokerConn,
		Broadcaster: b,
	})

	gp := graphql.NewPlayground(graphql.Options{
		URL: "/api/system/graphql/query",
	})

	port := viper.GetInt("PORT")
	r := router.NewRouter(ctx, h, port, g, gp)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		log.Info("Meshery Server listening on: ", port)
		if err := r.Run(); err != nil {
			log.Error(ErrListenAndServe(err))
			os.Exit(1)
		}
	}()
	<-c
	regManager.Cleanup()
	log.Info("Doing seeded content cleanup...")

	for _, p := range hc.Providers {
		// skipping none provider for now
		// so it doesn't throw error each server is stopped. Reason: support for none provider is not yet implemented
		if p.Name() != "None" {
			log.Info("De-registering Meshery server.")
			err = p.DeleteMesheryConnection()
			if err != nil {
				log.Error(err)
			}
		}
	}

	err = lProv.Cleanup()
	if err != nil {
		log.Error(ErrCleaningUpLocalProvider(err))
	}
	utils.DeleteSVGsFromFileSystem()
	log.Info("Closing database instance...")
	err = dbHandler.DBClose()
	if err != nil {
		log.Error(ErrClosingDatabaseInstance(err))
	}

	log.Info("Shutting down Meshery Server...")
}
