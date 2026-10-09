package main

import (
	"CalSync/graphhelper"
	"context"
	"log"
	"os"
)

func main() {
	err := graphhelper.Login(context.Background(), graphhelper.Config{
		TenantID:       os.Getenv("AZURE_TENANT_ID"),
		ClientID:       os.Getenv("AZURE_CLIENT_ID"),
		CacheName:      "CalSync",
		AuthRecordPath: `C:\ProgramData\CalSync\auth-record.json`,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("login ok")
}
