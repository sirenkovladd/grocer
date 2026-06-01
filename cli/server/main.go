package main

import (
	"encoding/json"
	"fmt"

	"code.sirenko.ca/grocer/lib/database"
	"code.sirenko.ca/grocer/lib/database/out_proto"
	"google.golang.org/protobuf/proto"
)

func main() {
	client, err := database.NewClient()
	if err != nil {
		panic(err)
	}
	client.CreateUser("", "admin", "adm")
	user, err := client.GetUser("admin")
	if err != nil {
		panic(err)
	}
	d1, _ := json.Marshal(user)
	fmt.Println(string(d1))

	data, err := proto.Marshal(user)
	if err != nil {
		panic(err)
	}
	fmt.Println(data, len(data))

	user2 := &out_proto.User{}
	if err := proto.Unmarshal(data, user2); err != nil {
		panic(err)
	}
	d2, _ := json.Marshal(user2)
	fmt.Println(string(d2))

	fmt.Println(client.LoginUser("admin", "adm1"))
	fmt.Println(client.LoginUser("admin1", "adm"))
	user, session, err := client.LoginUser("admin", "adm")
	if err != nil {
		panic(err)
	}
	fmt.Println(user, session)

	user3, err := client.GetUserBySession(session)
	if err != nil {
		panic(err)
	}
	fmt.Println(user3)

	uid := database.NewGenerator()
	for i := 0; i < 2; i++ {
		id := uid.Gen()
		fmt.Println(i, id)
		time, counter := database.ParseUID(id)
		fmt.Println(time, counter)
	}
}
