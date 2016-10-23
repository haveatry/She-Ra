package jobs

import (
	"fmt"
	"github.com/emicklei/go-restful"
	"github.com/golang/protobuf/proto"
	"github.com/haveatry/She-Ra/configdata"
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

type JobManager struct {
	JobMap     map[string]*configdata.Job
	JobExecMap map[string]chan int
	accessLock *sync.RWMutex
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

type JobCommand struct {
	Name string
	Args []string
}

func NewJobManager() *JobManager {
	jobManager := &JobManager{
		JobMap:     make(map[string]*configdata.Job, 100),
		JobExecMap: make(map[string]chan int, 100),
		accessLock: &sync.RWMutex{},
	}
	return jobManager
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

	job.MaxKeepDays = MAX_KEEP_DAYS
	job.MaxExecutionRecords = MAX_EXEC_NUM
	job.CurrentNumber = 0

	d.accessLock.RLock()
	_, OK := d.JobMap[job.Id]
	d.accessLock.RUnlock()
	if OK {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	} else {
		go d.execJobCmd(job.Id)
		d.accessLock.Lock()
		d.JobExecMap[job.Id] = make(chan int, 1)
		d.JobMap[job.Id] = job
		d.accessLock.Unlock()
	}

	//encode job info and store job info into config file
	out, err := proto.Marshal(job)
	if err != nil {
		log.Fatalln("Failed to encode job info:", err)
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	createWorkSpace := &JobCommand{
		Name: "mkdir",
		Args: []string{"-p", WS_PATH + job.Id + "/" + EXECUTION_PATH},
	}
	createWorkSpace.Exec()

	configFile := WS_PATH + job.Id + "/configfile"
	if err := ioutil.WriteFile(configFile, out, 0644); err != nil {
		log.Fatalln("Failed to write job info to file:", err)
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	response.WriteHeaderAndEntity(http.StatusCreated, job)
}

func (d *JobManager) findJob(request *restful.Request, response *restful.Response) {
	jobId := request.PathParameter("job-id")
	info("job.Id: %s\n", jobId)

	d.accessLock.RLock()
	info("Get the read lock successfully")
	job, OK := d.JobMap[jobId]
	d.accessLock.RUnlock()
	if OK {
		info("Get job successfully")
		response.WriteHeaderAndEntity(http.StatusFound, job)
	} else {
		info("failed to find the job %s\n", jobId)
		response.WriteHeader(http.StatusNotFound)
	}
}

func (d *JobManager) findAllJobs(request *restful.Request, response *restful.Response) {

}

func (d *JobManager) updateJob(request *restful.Request, response *restful.Response) {
	job := new(configdata.Job)
	err := request.ReadEntity(job)
	info("job.Id: %s\n", job.Id)
	if err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	job.MaxKeepDays = MAX_KEEP_DAYS
	job.MaxExecutionRecords = MAX_EXEC_NUM
	job.CurrentNumber = 0
	d.accessLock.Lock()
	d.JobMap[job.Id] = job
	d.accessLock.Unlock()

	response.WriteHeaderAndEntity(http.StatusCreated, job)
}

func (d *JobManager) delJob(request *restful.Request, response *restful.Response) {
	jobId := request.PathParameter("job-id")
	info("jober.Id: %s", jobId)
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
	response.WriteHeader(http.StatusAccepted)
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
	job, OK := d.JobMap[jobId]
	d.accessLock.RUnlock()
	if OK {
		info("Get job successfully")
		jobExec := &configdata.Execution{}
		jobExec.Number = job.CurrentNumber + 1
		now := time.Now()
		year, mon, day := now.Date()
		hour, min, sec := now.Clock()
		jobExec.LogFile = fmt.Sprintf("%03d-%d%02d%02d%02d%02d%02d", int(jobExec.Number), year, mon, day, hour, min, sec)
		jobExec.Progress = configdata.Execution_INIT
		jobExec.EndStatus = configdata.Execution_SUCCESS
		response.WriteHeaderAndEntity(http.StatusCreated, jobExec)

		d.accessLock.Lock()

		d.JobMap[jobId].CurrentNumber++
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
				job, OK := d.JobMap[jobId]
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

					if job.JdkVersion == "jdk1.7" {
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
					if codeManager := job.GetCodeManager(); codeManager != nil && codeManager.GitConfig != nil {
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

					if buildManager := job.GetBuildManager(); buildManager != nil {
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

					if job.BuildImgCmd != "" {
						cmdWithArgs := strings.Split(job.BuildImgCmd, " ")
						imgBuildCmd := &JobCommand{
							Name: cmdWithArgs[0],
							Args: cmdWithArgs[1:],
						}
						success = imgBuildCmd.Exec()
						if !success {
							break
						}
					}

					if job.PushImgCmd != "" {
						cmdWithArgs := strings.Split(job.PushImgCmd, " ")
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
				delete(d.JobMap, jobId)
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
