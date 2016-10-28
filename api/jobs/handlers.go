package jobs

import (
	"fmt"
	"github.com/emicklei/go-restful"
	"She-Ra/util/lrumap"
	"runtime"
	"path"
	"errors"
	"github.com/golang/protobuf/proto"
	"She-Ra/configdata"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	WS_PATH        = "/workspace/"
	EXECUTION_PATH = "executions/"
	MAX_EXEC_NUM   = 100
	MAX_KEEP_DAYS  = 3
	KILL_GOROUTINE = 886
	EXEC_GOROUTINE = 1314
)

type JobCommand struct {
	Name string
	Args []string
}

type JobManager struct {
    JobMap *lrumap.LRU
    accessLock *sync.RWMutex
    JobExecMap map[string]chan int
}

/*
type JobExecInternal struct {
	CommandList []string
	Finish      chan int
}
*/

/*
type GitClient struct {
}
*/

func NewJobManager(jobNum int, jobChan int) (*JobManager, error) {
	if jobNum <= 0 || jobChan <= 0 {
		return nil, errors.New("job num or job channel num is zero.")
	}
	lru, err := lrumap.NewLRU(jobNum, nil)
	if err != nil {
		return nil, errors.New("init lru map failed")
	}
	jobManager := &JobManager{
		JobMap:     lru,
		JobExecMap: make(map[string]chan int, jobChan),
		accessLock: &sync.RWMutex{},
	}
	return jobManager, nil
}

func (d *JobManager) createJob(request *restful.Request, response *restful.Response) {
	job := new(configdata.Job)
	err := request.ReadEntity(job)
	info("job.Id: %s\n", job.Id)
	if err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	
	//job.Id = strconv.Itoa(d.JobMap.Len() + 1)
	// map key id:name after 2016/10
	key := job.Id
	d.accessLock.Lock()
	if isExistJob(key) == true {
		d.accessLock.Unlock()
		response.WriteErrorString(http.StatusInternalServerError, "job key already exist, please retry." )
		return	
	}
	job.MaxKeepDays = MAX_KEEP_DAYS
	job.MaxExecutionRecords = MAX_EXEC_NUM
	job.CurrentNumber = 0
	
	if _, err := writeDataToFile(job); err != nil {
		d.accessLock.Unlock()
		log.Print("write job into file failed. key : ", key, "; ", job)
		response.WriteErrorString(http.StatusInternalServerError, "write job into file failed, please retry." )
		return 
	}else{
		go d.execJobCmd(job.Id)
		d.JobExecMap[job.Id] = make(chan int, 1)
		if d.JobMap.Add(job.Id, job) == false {
			d.accessLock.Unlock()
			response.WriteErrorString(http.StatusInternalServerError, ", please retry." )
			return
		}
		createWorkSpace := &JobCommand{
			Name: "mkdir",
			Args: []string{"-p", WS_PATH + job.Id + "/" + EXECUTION_PATH},
		}
	    createWorkSpace.Exec()

		// put some data to database
		
		log.Print("add new job succeed; key: ", key, "; jober: ", job)
	}
	d.accessLock.Unlock()
	response.WriteHeaderAndEntity(http.StatusCreated, job)
}

func writeDataToFile(job *configdata.Job) (fileName string, err error) {

	data, err := proto.Marshal(job)
	//data, err := json.Marshal(job)
	if err != nil {
        log.Fatal("marshaling error: ", err)
		return "", err	
	}
    log.Print("proto marshal: job", string(data))

	filePath := WS_PATH + job.Id + "/configfile"
	if err = os.MkdirAll(filePath, 0777); err != nil {
		return  "", err
	}
	logPath  := filePath + "/log"
	if err = os.MkdirAll(logPath, 0777); err != nil {
		return "", err
	}
	fileName = filePath + "/" + job.Id
	//log.Print("job fileName: ", fileName)
	var file *os.File
	if isFileExist(fileName) != true {
		if file, err = os.Create(fileName); os.IsNotExist(err) {
			log.Print("file create failed : ", err)
			return "", err
		}else{
			log.Print("create file successfully.")
		}
		
	}else{
		if file, err = os.OpenFile(fileName, os.O_RDWR | os.O_TRUNC, 0666); os.IsNotExist(err) {
			log.Print(err)
			return fileName, err
		}else{
			log.Print("OpenFile ", fileName, " successfully.")
		}
	}
	var nLen int
	if nLen, err = file.Write(data); err != nil {
		log.Print("write data ino file ", fileName, " failed : ", string(data), ";data len: ", nLen)
		return fileName, err
	}else{
		log.Print("write data into file succeed. file: ", fileName, "; data: ", string(data))
	}
	if err = file.Close(); err != nil {
		log.Print("file close failed: ", err)
		return fileName, err
	}else{
		log.Print("file close succeed.")	
	}
	return fileName, err
}

func getLocalPath() string {
	var filePath string
	_, fullFileName, _, ok := runtime.Caller(0)
	if ok != false {
		filePath = path.Dir(fullFileName)
	}
	log.Print("get path :", filePath)
	return filePath
}

func isFileExist(fileName string) bool {
	var bExist bool
	if _, err := os.Stat(fileName); os.IsNotExist(err){
		log.Print("file is not exist, ", err)
		bExist = false 
	}else{
		bExist = true
		log.Print("file is exist.")
	}
	return bExist
}

func (d *JobManager) findAllJobs(request *restful.Request, response *restful.Response) {

}

func (d *JobManager) findJob(request *restful.Request, response *restful.Response) {
	job_key := request.PathParameter("job-id")
	
	// first read data from database
	if ok := isExistJob(job_key); ok != false {
		errResponse(http.StatusNotFound, "job-id: " + job_key +  " not find in database.", response)
		return 
	}
	d.accessLock.RLock()
	if  ok := d.JobMap.Contains(job_key); ok != true {
		// read data from file
		fName := WS_PATH + job_key + "/" + "configure"
		if job, err := readJobFromFile(fName); err != nil {
			d.accessLock.RUnlock()
			errResponse(http.StatusNotFound, "job-id: " + job_key +  " in database and file, not in memery; " + err.Error(), response)
			return
		}else{
			if d.JobMap.Add(job_key, job) == false {
				d.accessLock.RUnlock()
				errResponse(http.StatusNotFound, "job-id: " + job_key +  "; get job from memery failed.", response)
			}else{
				d.accessLock.RUnlock()
				response.WriteHeaderAndEntity(http.StatusFound, job)
			}
			return
		}
	}
}

func readJobFromFile(fName string) (*configdata.Job, error) {
	if len(fName) == 0 {
		log.Print("fileName is empty.")
		return nil, errors.New("fileName is empty")
	}
	in, err := ioutil.ReadFile(fName)
	if err != nil {
        	log.Fatalln("Error reading file:", err)
		return nil, err
	}
	jObj := &configdata.Job{}
	if err := proto.Unmarshal(in, jObj); err != nil {
        	log.Fatalln("Failed to parse job:", err)
		return nil, err
	}
	return jObj, nil
	
} 

func (d *JobManager) updateJob(request *restful.Request, response *restful.Response) {
	job_key := request.PathParameter("job-id")
	
	job := new(configdata.Job)
	err := request.ReadEntity(job)
	if err != nil {
		errResponse(http.StatusInternalServerError, err.Error(), response)
		return
	}
	
	if ok := isExistJob(job_key); ok != true {
		errResponse(410, "job is not exist.", response)
	}else{
		if fileName, err := writeDataToFile(job); err != nil {
			errResponse(412, err.Error() + "; fileName:" + fileName, response)
		}else{
			job.MaxKeepDays = MAX_KEEP_DAYS
			job.MaxExecutionRecords = MAX_EXEC_NUM
			job.CurrentNumber = 0
			d.accessLock.Lock()
			if d.JobMap.Add(job_key, job) == false {
				log.Print("add to mem cache failed: job_key: ", job_key)
			}
			d.accessLock.Unlock()	
			log.Print("add new job succeed; key: ", job_key, "; jober: ", job)
			response.WriteHeaderAndEntity(http.StatusOK, job)
		}
	}
}

func (d *JobManager) delJob(request *restful.Request, response *restful.Response) {
	jobId := request.PathParameter("job-id")
	if ok := isExistJob(jobId); ok != true {
		errResponse(410, "job is not exist.", response)
	}else{
		d.setChan(jobId, KILL_GOROUTINE)
             	cleanupCmd := &JobCommand{
                     Name: "rm",
                     Args: []string{"-rf", WS_PATH + jobId},
             	}
             	success := cleanupCmd.Exec()
             	if !success {
                     response.WriteHeader(http.StatusInternalServerError)
                     return
             	}
     
             	d.setChan(jobId, KILL_GOROUTINE)

		d.accessLock.Lock()
		if ok = d.JobMap.Contains(jobId); ok == true {
			d.JobMap.Remove(jobId)
		}
		d.accessLock.Unlock()
		if err := os.RemoveAll(WS_PATH + jobId); err != nil {
			log.Print("when remove job , something occur: ", err)
		}
		// del from database
		
		response.WriteHeaderAndEntity(http.StatusOK, "remove job succeed.")
	}
}

func errResponse(status int, errInfo string,  response *restful.Response) {
	response.AddHeader("Content-Type", "text/plain")
	response.WriteErrorString(status, errInfo)
}

func isExistJob(job_id string) bool {
	// first read data from database
	return false
}

func (d *JobManager) setChan(jobId string, value int) {

	info("jobId is %s\n", jobId)
	d.JobExecMap[jobId] <- value
}

func (d *JobManager) execJob(request *restful.Request, response *restful.Response) {
	jobId := request.PathParameter("job-id")
	info("jober.Id: %s\n", jobId)

	d.accessLock.RLock()
	info("Get the read lock successfully")
	job, OK := d.JobMap.Get(jobId)
	d.accessLock.RUnlock()
	if OK {
		info("Get job successfully")
		jobExec := &configdata.Execution{}
		jobExec.Number = job.(configdata.Job).CurrentNumber + 1
		now := time.Now()
		year, mon, day := now.Date()
		hour, min, sec := now.Clock()
		jobExec.LogFile = fmt.Sprintf("%03d-%d%02d%02d%02d%02d%02d", int(jobExec.Number), year, mon, day, hour, min, sec)
		jobExec.Progress = configdata.Execution_INIT
		jobExec.EndStatus = configdata.Execution_SUCCESS
		response.WriteHeaderAndEntity(http.StatusCreated, jobExec)

		d.accessLock.Lock()
	        	
		job.(*configdata.Job).CurrentNumber++
		//go d.execJobCmd(jobId)
		info("Get the write lock successfully")
		d.accessLock.Unlock()
		go d.setChan(jobId, EXEC_GOROUTINE)
	} else {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, "no such job found")
		return
	}
	/*
		configFile := WS_PATH + jobId
		in, err := ioutil.ReadFile(configFile)
		if err != nil {
			if os.IsNotExist(err) {
				log.Println("%s: File not found.  Creating new file.\n", configFile)

			} else {
				log.Fatalln("Error reading file:", err)
			}
			response.AddHeader("Content-Type", "text/plain")
			response.WriteErrorString(http.StatusInternalServerError, err.Error())
		}

		job := &configdata.Job{}
		if err := proto.Unmarshal(in, job); err != nil {
			log.Fatalln("Failed to parse job info:", err)
		}
	*/
}

func (cmd *JobCommand) Exec() bool {
	var (
		cmdOut []byte
		err    error
	)
	if cmdOut, err = exec.Command(cmd.Name, cmd.Args...).Output(); err != nil {
		info("Failed to execute command: (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args)
		return false
	}
	info("Output (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args, string(cmdOut))
	return true
}

func (cmd *JobCommand) ExecPipeCmd(in *JobCommand) bool {
	var err error

	producer := exec.Command(in.Name, in.Args...)
	consumer := exec.Command(cmd.Name, cmd.Args...)
	if consumer.Stdin, err = producer.StdoutPipe(); err != nil {
		info("Failed to combine the 2 commands with pipe\n")
		return false
	}

	if err = consumer.Start(); err != nil {
		info("err occurred when start executing command: (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args)
		return false

	}

	if err = producer.Run(); err != nil {
		info("err occurred when executing command: (cmd=%s, agrs=%v): \\n%v\\n", in.Name, in.Args)
		return false

	}

	if err = consumer.Wait(); err != nil {
		info("err occurred when waiting the command executing complete: (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args)
		return false

	}

	return true

}

func (d *JobManager) execJobCmd(jobId string) {
	var success bool
	for {
		time.Sleep(5 * time.Second)
		info("test jobId is %s", jobId)

		select {
		case cmd, Closed := <-d.JobExecMap[jobId]:
			if !Closed {
				info("channel has been closed\n")
				break
			}

			info("Enter select")
			if cmd == 0 {
				//this job has been deleted, exit this goroutine
				info("command value is %d\n", cmd)
				break
			}

			if cmd == EXEC_GOROUTINE {
				//time.Sleep(600 * time.Second)
				d.accessLock.RLock()
				job, OK := d.JobMap.Get(jobId)
				var mJob *configdata.Job = job.(*configdata.Job)
				d.accessLock.RUnlock()
				if OK {
					info("begin to execute command")

					//change the working dir
					targetPath, err := filepath.Abs(WS_PATH + jobId)
					if err != nil {
						info("AbsError (%s): %s\\n", WS_PATH+jobId, err)
						break
					}

					info("Target Path: %s\\n", targetPath)
					err = os.Chdir(targetPath)
					if err != nil {
						info("ChdirError (%s): %s\\n", targetPath, err)
						break
					}

					//select correct jdk version
					echoCmd := &JobCommand{
						Name: "echo",
						Args: []string{"1"},
					}

					if mJob.JdkVersion == "jdk1.7" {
						echoCmd.Args = []string{"2"}
					}

					switchJdkCmd := &JobCommand{
						Name: "alternatives",
						Args: []string{"--config", "java"},
					}
					success = switchJdkCmd.ExecPipeCmd(echoCmd)
					if !success {
						break
					}

					//pull code from git
					if codeManager := job.(*configdata.Job).GetCodeManager(); codeManager != nil && codeManager.GitConfig != nil {
						info("begin to pulling code\n")
						gitInitCmd := &JobCommand{
							Name: "git",
							Args: []string{"init"},
						}
						success = gitInitCmd.Exec()
						if !success {
							break
						}

						gitConfigCmd := &JobCommand{
							Name: "git",
							Args: []string{"config", "remote.origin.url", codeManager.GitConfig.Repo.Url},
						}
						success = gitConfigCmd.Exec()
						if !success {
							break
						}

						gitPullCmd := &JobCommand{
							Name: "git",
							Args: []string{"pull", "origin", codeManager.GitConfig.Branch},
						}
						success = gitPullCmd.Exec()
						if !success {
							break
						}

					}

					if buildManager := mJob.GetBuildManager(); buildManager != nil {
						if buildManager.AntConfig != nil {
							antBuildCmd := &JobCommand{
								Name: "ant",
								Args: []string{"-f", buildManager.AntConfig.BuildFile, "-D" + buildManager.AntConfig.Properties},
							}
							success = antBuildCmd.Exec()
							if !success {
								break
							}
						}

						if buildManager.MvnConfig != nil {
							mvnBuildCmd := &JobCommand{
								Name: "mvn",
								Args: []string{"-f", buildManager.MvnConfig.Pom, buildManager.MvnConfig.Goals},
							}

							success = mvnBuildCmd.Exec()
							if !success {
								break
							}
						}
					}

					if mJob.BuildImgCmd != "" {
						cmdWithArgs := strings.Split(mJob.BuildImgCmd, " ")
						imgBuildCmd := &JobCommand{
							Name: cmdWithArgs[0],
							Args: cmdWithArgs[1:],
						}
						success = imgBuildCmd.Exec()
						if !success {
							break
						}
					}

					if mJob.PushImgCmd != "" {
						cmdWithArgs := strings.Split(mJob.PushImgCmd, " ")
						imgPushCmd := &JobCommand{
							Name: cmdWithArgs[0],
							Args: cmdWithArgs[1:],
						}
						success = imgPushCmd.Exec()
						if !success {
							break
						}
					}
				}
			}

			if cmd == KILL_GOROUTINE {
				info("The log file has been deleted, exit the goroutine\n")
				d.accessLock.Lock()
				d.JobMap.Remove(jobId)
				delete(d.JobExecMap, jobId)
				d.accessLock.Unlock()
				return
			}
		default:
			info("Enter default")
		}

	}
}

//watch one job execution status change
func (d *JobManager) watchJobExecution(request *restful.Request, response *restful.Response) {

}

//open on job execution record
func (d *JobManager) openJobExecution(request *restful.Request, response *restful.Response) {

}

//get the job execution list
func (d *JobManager) getAllJobExecutions(request *restful.Request, response *restful.Response) {

}

//delete one job execution record
func (d *JobManager) delJobExecution(request *restful.Request, response *restful.Response) {

}

//force stop one job execution
func (d *JobManager) killJobExecution(request *restful.Request, response *restful.Response) {

}

// Log wrapper
func info(template string, values ...interface{}) {
	log.Printf("[She-Ra][info] "+template+"\n", values...)
}
