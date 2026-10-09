package CalSync

import (
	envhelper "CalSync/EnvHelper"
	"CalSync/calservice"
	"log"
	"os"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

func main() {
	if err := envhelper.LoadEnv(true); err != nil {
		log.Printf("no .env loaded: %v", err)
	}

	f, err := os.OpenFile(`C:\ProgramData\CalSync\calsync.log`,
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		defer f.Close()
		log.SetOutput(f)
	}

	isSvc, err := svc.IsWindowsService()
	if err != nil {
		log.Fatal(err)
	}

	if isSvc {
		err = svc.Run(calservice.ServiceName, &calservice.CalService{})
	} else {
		log.SetOutput(os.Stderr)
		err = debug.Run(calservice.ServiceName, &calservice.CalService{})
	}
	if err != nil {
		log.Fatal(err)
	}
}
