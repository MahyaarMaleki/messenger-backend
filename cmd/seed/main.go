package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jaswdr/faker/v2"
	"github.com/mahyaarmaleki/messenger-backend/internal/config"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("cannot load config: ", err)
	}

	connPool, err := db.NewConnection(cfg.DBSource)
	if err != nil {
		log.Fatal(err)
	}

	defer connPool.Close()

	store := db.NewStore(connPool)
	fake := faker.New()

	fmt.Println("Seeding database...")

	password := "secret123"
	hashedPassword, err := util.HashPassword(password)
	if err != nil {
		log.Fatal("cannot hash password:", err)
	}

	for i := 0; i < 100; i++ {
		createRandomUser(store, fake, hashedPassword)
	}

	fmt.Println("Seeding finished!")
}

func createRandomUser(store *db.Store, f faker.Faker, hashedPw string) {
	// Generate unique-ish data
	firstName := f.Person().FirstName()
	lastName := f.Person().LastName()

	// Ensure username meets length constraint
	username := fmt.Sprintf("%s_%s_%d", firstName, lastName, f.IntBetween(1, 10000))
	if len(username) > 30 {
		username = username[:30]
	}

	arg := db.CreateUserParams{
		Username:     username,
		PasswordHash: hashedPw,
		Email:        f.Internet().Email(),
		FirstName:    firstName,
		LastName:     lastName,
	}

	// Handle Nullable fields (Bio, Avatar)
	// We use the sql.NullString helper or your preferred method
	// arg.Bio = sql.NullString{String: f.Lorem().Sentence(5), Valid: true}

	user, err := store.CreateUser(context.Background(), arg)
	if err != nil {
		// It's common to get duplicates in seeding (random collision)
		fmt.Printf("Skipping user %s: %v\n", username, err)
		return
	}

	fmt.Printf("Created user: %s (Password: %s)\n", user.Username, "secret123")
}
