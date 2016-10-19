package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"
	"github.com/emicklei/go-restful"
	"github.com/emicklei/go-restful/swagger"
	"github.com/haveatry/api/jobs"
	"github.com/haveatry/She-Ra/configdata"
	"github.com/magiconair/properties"
)

/*
type CodeManager struct {
	configId	uint
	gitAddr		string
	gitBranch	string
	refreshPolicy	bool
}

type CodeCompiling struct {
	configId	uint
	compileCmd	string
	compiler	string
	envVariables	string
	rebuildPolicy	bool
}

type ImageBuilding struct {
	configId	uint
	dockerFile	string
	registryAddr	string
	rebuildPolicy	uint
}

type TaskConfig struct {
	taskId			uint
	funcModules		[]uint
	executeTime		string
	dependency		[]uint
	maxExecutionRecords	uint
	maxKeepDays		uint
}

type TaskExec struct {
	executionId	uint
	result		uint
	execLog		string
}
*/
var (
	props          *properties.Properties
	propertiesFile = flag.String("config", "she-ra.properties", "the configuration file")

	SwaggerPath string
	SheRaIcon    string
)

func main() {
	flag.Parse()

	// Load configurations from a file
	info("loading configuration from [%s]", *propertiesFile)
	var err error
	if props, err = properties.LoadFile(*propertiesFile, properties.UTF8); err != nil {
		log.Fatalf("[She-Ra][error] Unable to read properties:%v\n", err)
	}
	
	// Swagger configuration
	SwaggerPath = props.GetString("swagger.path", "")
	SheRaIcon = filepath.Join(SwaggerPath, "images/jion.ico")

	// New Job Manager
	jobMng := &jobs.JobManager{
		JobMap: make(map[string]*configdata.Job),
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

