package main

import (
	"flag"
	"github.com/emicklei/go-restful"
	"github.com/emicklei/go-restful/swagger"
	"github.com/haveatry/She-Ra/api/jobs"
	"github.com/haveatry/She-Ra/utils"
	"github.com/magiconair/properties"
	"log"
	"net/http"
	"path/filepath"
)

var (
	props          *properties.Properties
	propertiesFile = flag.String("config", "she-ra.properties", "the configuration file")

	SwaggerPath string
	SheRaIcon   string
)

func main() {
	flag.Parse()

	// Load configurations from a file
	info("loading configuration from [%s]", *propertiesFile)
	var err error
	var jobMng *jobs.JobManager
	if props, err = properties.LoadFile(*propertiesFile, properties.UTF8); err != nil {
		log.Fatalf("[She-Ra][error] Unable to read properties:%v\n", err)
	}

	// Swagger configuration
	SwaggerPath = props.GetString("swagger.path", "")
	SheRaIcon = filepath.Join(SwaggerPath, "images/jion.ico")

	// init database
	utils.Init()

	// New Job Manager
	if jobMng, err = jobs.NewJobManager(); err != nil {
		log.Fatalf("[She-Ra][error] failed to create JobManager:%v\n", err)
	}

	// accept and respond in JSON unless told otherwise
	restful.DefaultRequestContentType(restful.MIME_JSON)
	restful.DefaultResponseContentType(restful.MIME_JSON)

	// faster router
	restful.DefaultContainer.Router(restful.CurlyRouter{})
	// no need to access body more than once
	restful.SetCacheReadEntity(false)

	// API Cross-origin requests
	apiCors := props.GetBool("http.server.cors", false)

	//Register API
	jobs.Register(jobMng, restful.DefaultContainer, apiCors)

	addr := props.MustGet("http.server.host") + ":" + props.MustGet("http.server.port")
	basePath := "http://" + addr

	// Register Swagger UI
	swagger.InstallSwaggerService(swagger.Config{
		WebServices:     restful.RegisteredWebServices(),
		WebServicesUrl:  basePath,
		ApiPath:         "/apidocs.json",
		SwaggerPath:     SwaggerPath,
		SwaggerFilePath: props.GetString("swagger.file.path", ""),
	})

	log.Print("basePath: ", basePath, "SwaggerPath: ", SwaggerPath, "SheRaIcon: ", SheRaIcon)
	// If swagger is not on `/` redirect to it
	if SwaggerPath != "/" {
		http.HandleFunc("/", index)
	}

	// Serve favicon.ico
	http.HandleFunc("/favion.ico", icon)
	//http.Handle("/log", websocket.Handler(WatchLog))

	info("ready to serve on %s", basePath)
	log.Fatal(http.ListenAndServe(addr, nil))

}

// If swagger is not on `/` redirect to it
func index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, SwaggerPath, http.StatusMovedPermanently)
}

func icon(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, SheRaIcon, http.StatusMovedPermanently)
}

// Log wrapper
func info(template string, values ...interface{}) {
	log.Printf("[She-Ra][info] "+template+"\n", values...)
}
