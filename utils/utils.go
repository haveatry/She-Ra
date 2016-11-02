package utils

import (
	"database/sql"
	"github.com/golang/protobuf/proto"
	"github.com/haveatry/She-Ra/configdata"
	_ "github.com/mattn/go-sqlite3"
	"io/ioutil"
	"log"
	"os"
	"path"
	"runtime"
)

const (
	WS_PATH           = "/workspace/"
	EXECUTION_PATH    = "executions/"
	MAX_EXEC_NUM      = 100
	MAX_KEEP_DAYS     = 3
	EXEC_GOROUTINE    = 1314
	EXEC_FINISHED     = 1
	EXEC_KILL_ALL     = 886
	EXEC_KILL_FAILURE = 8
	EXEC_ERROR        = 16
)

var Database *sql.DB

type Key struct {
	Ns string
	Id string
}

func Init() {
	//create database for She-Ra project
	var err error
	if Database, err = sql.Open("sqlite3", "/workspace/She-Ra.db"); err != nil {
		info("failed to setup database")
	}

	//create table job to store execution information
	sql := `Create table IF NOT EXISTS job(namespace string(100), jobId string(100),number int,duration int,progress int,status int, finished int, cancelled int)`
	if _, err = Database.Exec(sql); err != nil {
		info("failed to create table job")
	}
}

func InsertExecutionRecord(namespace, jobId string, number, duration, progress, status int) {
	if stmt, err := Database.Prepare("insert into job(namespace, jobId, number, duration, progress, status,finished, cancelled) values (?,?,?,?,?,?,?,?)"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare insert sql:%v\n", err)
	} else if _, err := stmt.Exec(namespace, jobId, number, duration, progress, status, 0, 0); err != nil {
		log.Fatalf("[She-Ra][error] failed to insert data into database:%v\n", err)
	}
}

func UpdateExecutionRecord(namespace, jobId string, number, duration, progress, status, finished int) {
	if stmt, err := Database.Prepare("update job set duration=?, progress=?, status=?, finished=? where namespace=? and jobId=? and number=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare update sql:%v\n", err)
	} else if _, err := stmt.Exec(duration, progress, status, finished, namespace, jobId, number); err != nil {
		log.Fatalf("[She-Ra][error] failed to update data in database:%v\n", err)
	}
}

func SetExecutionCancelled(namespace, jobId string, number int) {
	if stmt, err := Database.Prepare("update job set cancelled=1 where namespace=? and jobId=? and number=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare update sql:%v\n", err)
	} else if _, err := stmt.Exec(namespace, jobId, number); err != nil {
		log.Fatalf("[She-Ra][error] failed to set execution cancelled in database:%v\n", err)
	}
}

func SetAllCancelled(namespace, jobId string) {
	if stmt, err := Database.Prepare("update job set cancelled=1 where namespace=? and jobId=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare update sql:%v\n", err)
	} else if _, err := stmt.Exec(namespace, jobId); err != nil {
		log.Fatalf("[She-Ra][error] failed to set all cancelled in database:%v\n", err)
	}
}

func GetCancelStatus(namespace, jobId string, number int) int {
	var cancelStat int
	if stmt, err := Database.Prepare("select cancelled from job where namespace=? and jobId=? and number=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare update sql:%v\n", err)
	} else if err := stmt.QueryRow(namespace, jobId, number).Scan(&cancelStat); err != nil {
		log.Fatalf("[She-Ra][error] failed to get cancel stat in database:%v\n", err)
	}
	return cancelStat
}

func DeleteExecutionRecord(namespace, jobId string, number int) {
	if stmt, err := Database.Prepare("delete from job where namespace=? and jobId=? and number=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare delete sql:%v\n", err)
	} else if _, err := stmt.Exec(namespace, jobId, number); err != nil {
		log.Fatalf("[She-Ra][error] failed to delete job execution in database:%v\n", err)
	}
}

func DeleteJobExecutions(namespace, jobId string) {
	if stmt, err := Database.Prepare("delete from job where namespace=? and jobId=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare delete sql:%v\n", err)
	} else if _, err := stmt.Exec(namespace, jobId); err != nil {
		log.Fatalf("[She-Ra][error] failed to delete job executions in database:%v\n", err)
	}
}

func getRunningCount(namespace, jobId string, finished int) int {
	var count int
	if stmt, err := Database.Prepare("select count(*) from job where namespace=? and jobId =? and finished=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare query sql:%v\n", err)
	} else if err := stmt.QueryRow(namespace, jobId, finished).Scan(&count); err != nil {
		log.Fatalf("[She-Ra][error] failed to get the tatoal running executions:%v\n", err)
	}
	return count
}

func Contains(key Key) bool {
	var count int
	if stmt, err := Database.Prepare("select count(*) from job where namespace=? and jobId =?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare query sql:%v\n", err)
	} else if err := stmt.QueryRow(key.Ns, key.Id).Scan(&count); err != nil {
		log.Fatalf("[She-Ra][error] failed to get the tatoal running executions:%v\n", err)
	}

	if count > 0 {
		return true
	} else {
		return false
	}
}

func ReadData(key Key, job *configdata.Job) error {
	fileName := WS_PATH + key.Ns + "/" + key.Id + "/.shera/configfile"
	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		if os.IsNotExist(err) {
			info("%s: File not found.  Creating new file.\n", fileName)

		} else {
			log.Fatalln("[She-Ra][error]Error reading file:", err)

		}

	}

	if err := proto.Unmarshal(data, job); err != nil {
		log.Fatalln("Failed to parse address book:", err)
	}
	return err
}

func WriteData(key Key, job *configdata.Job) error {
	var (
		data []byte
		err  error
		file *os.File
	)

	if data, err = proto.Marshal(job); err != nil {
		log.Fatalf("[She-Ra][error] marshling error:%v\n", err)
		return err
	}

	info("proto marshal: job", string(data))
	fileName := WS_PATH + key.Ns + "/" + key.Id + "/.shera/configfile"

	if file, err = os.OpenFile(fileName, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0666); err != nil {
		log.Fatalf("[She-Ra][error] failed to open file:%v\n", err)
		return err
	} else {
		info("OpenFile ", fileName, " successfully.")
	}

	if _, err = file.Write(data); err != nil {
		info("write data ino file %s failed\n", fileName)
		return err
	} else {
		info("write data into file succeed. file: ", fileName, "; data: ", string(data))
	}

	if err = file.Close(); err != nil {
		info("file close failed: ", err)
		return err
	} else {
		info("file close succeed.")
	}
	return err
}

func FileExists(fileName string) bool {
	var bExist bool
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		log.Print("file is not exist, ", err)
		bExist = false
	} else {
		bExist = true
		log.Print("file is exist.")
	}
	return bExist
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

func info(template string, values ...interface{}) {
	log.Printf("[She-Ra][info] "+template+"\n", values...)
}
