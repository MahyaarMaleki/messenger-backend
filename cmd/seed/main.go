package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jaswdr/faker/v2"
	"github.com/mahyaarmaleki/messenger-backend/internal/config"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
)

func main() {
	seedUsers := flag.Bool("users", false, "Seed 100 random users")
	seedChannel := flag.Bool("channel", false, "Seed the dummy sports channel")
	flag.Parse()

	if !*seedUsers && !*seedChannel {
		fmt.Println("Please specify what to seed.")
		fmt.Println("Usage: go run cmd/seed/main.go -users -channel")
		return
	}

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

	if *seedUsers {
		fmt.Println("Seeding 100 random users...")
		for i := 0; i < 100; i++ {
			createRandomUser(store, fake, hashedPassword)
		}
	}

	if *seedChannel {
		fmt.Println("Seeding Sports Channel...")
		seedSportsChannel(store, hashedPassword)
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

func seedSportsChannel(store *db.Store, hashedPw string) {
	ctx := context.Background()

	// 1. Create the Admin User (The "Journalist")
	admin, err := store.CreateUser(ctx, db.CreateUserParams{
		Username:     "football_daily",
		PasswordHash: hashedPw,
		Email:        "admin@footballdaily.com",
		FirstName:    "Fabrizio",
		LastName:     "Romano",
	})
	if err != nil {
		fmt.Println("Sports admin already exists or error:", err)
		return
	}

	// 2. Create the Channel
	channel, err := store.CreateConversation(ctx, db.CreateConversationParams{
		Type: "channel",
		Name: pgtype.Text{
			Valid:  true,
			String: "Live: Champions League Final",
		},
	})
	if err != nil {
		log.Fatal("Failed to create channel:", err)
	}

	// 3. Add the Admin to the Channel
	_, err = store.AddParticipant(ctx, db.AddParticipantParams{
		ConversationID: channel.ID,
		UserID:         admin.ID,
		Role:           "creator",
	})
	if err != nil {
		log.Fatal("Failed to add admin to channel:", err)
	}

	// 4. The 50-Message Dataset (Chronological Match Commentary)
	messages := []string{
		"Welcome to the Live Text Commentary for the UEFA Champions League Final!",
		"Tonight's clash features Manchester City against Real Madrid.",
		"The atmosphere at Wembley is absolutely electric. 90,000 fans are in the stands.",
		"Team news is in: De Bruyne starts for City, while Vinicius Jr leads the line for Madrid.",
		"Kick-off is just 5 minutes away. The players are in the tunnel.",
		"We are underway! Real Madrid kicks off from left to right.",
		"2' Early possession for City. They are knocking the ball around the back.",
		"5' FOUL! Rodri brings down Bellingham in the midfield. Free kick Madrid.",
		"8' CHANCE! Haaland gets a header on target but Courtois makes a comfortable save.",
		"12' Real Madrid is sitting deep, trying to absorb the pressure.",
		"15' Vinicius breaks on the counter! He beats Walker but his cross is cleared by Dias.",
		"18' City dominating possession with 68% so far.",
		"22' De Bruyne tries a shot from outside the box... deflected for a corner.",
		"23' The corner comes to nothing. Madrid clears.",
		"27' YELLOW CARD. Carvajal goes into the book for a late challenge on Grealish.",
		"31' It's a very tactical game so far. Neither side wants to make a mistake.",
		"34' GOALLLL!!! MANCHESTER CITY 1-0 REAL MADRID!",
		"35' Erling Haaland breaks the deadlock! A beautiful through ball from De Bruyne.",
		"38' Madrid looks stunned. They need to change their game plan now.",
		"41' Bellingham tries to orchestrate an attack but City's midfield is too compact.",
		"45' There will be 2 minutes of added time.",
		"45+2' HALF TIME: Manchester City 1 - 0 Real Madrid.",
		"Analysis: City fully deserves the lead. Madrid has offered very little going forward.",
		"The players are back out. No changes at the break for either side.",
		"46' Second half begins! City kicks off.",
		"50' Madrid is pressing much higher up the pitch now.",
		"53' CHANCE! Rodrygo hits the post! Ederson was completely beaten.",
		"56' That was a massive wake-up call for Manchester City.",
		"60' SUBSTITUTION (Madrid): Modric replaces Kroos in midfield.",
		"64' The game is really opening up now. End to end action.",
		"68' PENALTY TO REAL MADRID! Dias brings down Vinicius in the box!",
		"69' VAR is checking the challenge... Penalty stands!",
		"70' GOAL!!! MANCHESTER CITY 1-1 REAL MADRID!",
		"71' Vinicius Jr slots it calmly down the middle. We are level!",
		"75' SUBSTITUTION (City): Foden comes on for Grealish.",
		"78' The momentum has completely shifted to the Spanish giants.",
		"82' Modric plays a beautiful trivela pass, but Bellingham's shot is blocked.",
		"85' Just five minutes of normal time remaining. Are we heading to extra time?",
		"88' Foden drives down the wing, wins a corner for City.",
		"89' De Bruyne swings it in... Cleared by Rudiger.",
		"90' There will be 4 minutes of stoppage time.",
		"90+1' Madrid on the counter attack! This looks dangerous!",
		"90+2' GOALLLLLLL!!!!! UNBELIEVABLE SCENES! REAL MADRID LEAD 2-1!",
		"90+3' Jude Bellingham with an absolute screamer from 25 yards out! Top corner!",
		"90+4' City players have dropped to their knees. Heartbreak for the English side.",
		"90+5' FULL TIME! REAL MADRID WINS THE CHAMPIONS LEAGUE!",
		"What an incredible comeback from Los Blancos. They secure their 15th European title.",
		"Jude Bellingham is the hero tonight with a 92nd-minute winner.",
		"Heartbreak for Haaland and City, who dominated the first half.",
		"Thanks for joining our live coverage. Goodnight from Wembley!",
	}

	// 5. Insert messages sequentially
	fmt.Println("Seeding Sports Channel with 50 messages...")
	for _, content := range messages {
		_, err := store.CreateMessage(ctx, db.CreateMessageParams{
			ConversationID: channel.ID,
			SenderID:       admin.ID,
			Content:        content,
		})
		if err != nil {
			fmt.Printf("Error inserting message: %v\n", err)
		}

		// CRITICAL: A tiny sleep ensures Postgres inserts them with distinct timestamps.
		// If they all have the exact same millisecond, the ORDER BY clause might scramble them!
		time.Sleep(5 * time.Millisecond)
	}

	fmt.Printf("Successfully created Channel '%s' with 50 messages.\n", channel.Name)
}
