package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

// Define the struct for the GitHub API response
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Define the struct for the request body
type RequestBody struct {
	ShortName string `json:"shortName"`
	Github    string `json:"github"`
	Filter    string `json:"filter"`
}

// Define the struct for the repository item
type Repository struct {
	ShortName string `json:"shortName"`
	GithubURL string `json:"githubURL"`
	Filter    string `json:"filter"`
}

var db *sql.DB

func init() {
	var err error

	// Read database configuration from environment variables
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	dbname := os.Getenv("DB_NAME")

	// Construct the PostgreSQL connection string
	connStr := fmt.Sprintf("user=%s password=%s host=%s port=%s dbname=%s sslmode=disable",
		user, password, host, port, dbname)

	fmt.Println(connStr)

	// Connect to PostgreSQL
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
}

func fetchGitHubData(repoURL string) ([]Release, error) {
	resp, err := http.Get(repoURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch data from GitHub: %s", resp.Status)
	}

	var releases []Release
	err = json.NewDecoder(resp.Body).Decode(&releases)
	if err != nil {
		return nil, err
	}

	return releases, nil
}

func newHandler(c *gin.Context) {
	var body RequestBody
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	shortName := body.ShortName
	githubURL := body.Github
	filter := body.Filter

	// Insert the data into the database
	_, err := db.Exec("INSERT INTO repositories (short_name, github_url, filter) VALUES ($1, $2, $3)", shortName, githubURL, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert data into the database"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Data inserted successfully"})
}

func versionHandler(c *gin.Context) {
	shortName := c.Param("name")

	// Query the database for the GitHub repository URL associated with the shortName
	var repoURL string
	err := db.QueryRow("SELECT github_url FROM repositories WHERE short_name = $1", shortName).Scan(&repoURL)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Repository not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query error"})
		}
		return
	}

	// Fetch releases from the GitHub API
	releases, err := fetchGitHubData(repoURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data from GitHub"})
		return
	}

	// Return the latest version tag
	if len(releases) > 0 {
		latestVersion := releases[0].TagName
		c.String(http.StatusOK, latestVersion)
	} else {
		c.JSON(http.StatusNotFound, gin.H{"error": "No versions found"})
	}
}

func downloadHandler(c *gin.Context) {
	shortName := c.Param("name")

	// Query the database for the GitHub repository URL and filter criteria associated with the shortName
	var repoURL, filter string
	err := db.QueryRow("SELECT github_url, filter FROM repositories WHERE short_name = $1", shortName).Scan(&repoURL, &filter)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Repository not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query error"})
		}
		return
	}

	// Fetch releases from the GitHub API
	releases, err := fetchGitHubData(repoURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch data from GitHub"})
		return
	}

	// Find the asset with a name containing the filter string
	for _, release := range releases {
		for _, asset := range release.Assets {
			if strings.Contains(asset.Name, filter) {
				c.Redirect(http.StatusSeeOther, asset.BrowserDownloadURL)
				return
			}
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "No asset found matching the filter"})
}

func listHandler(c *gin.Context) {
	rows, err := db.Query("SELECT short_name, github_url, filter FROM repositories")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query the database"})
		return
	}
	defer rows.Close()

	var repositories []Repository
	for rows.Next() {
		var repo Repository
		if err := rows.Scan(&repo.ShortName, &repo.GithubURL, &repo.Filter); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan database rows"})
			return
		}
		repositories = append(repositories, repo)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error occurred while iterating rows"})
		return
	}

	c.JSON(http.StatusOK, repositories)
}

// LoggerMiddleware logs details of each request
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Log the request details
		log.Printf("Started %s %s", c.Request.Method, c.Request.URL.Path)

		// Process the request
		c.Next()

		// Log the response status and duration
		duration := time.Since(start)
		status := c.Writer.Status()
		log.Printf("Completed %s %s with status %d in %v", c.Request.Method, c.Request.URL.Path, status, duration)
	}
}

// Function to perform the health check
func performHealthCheck() {
	for {
		err := db.Ping()
		if err != nil {
			log.Printf("Health check failed: %v\n", err)
		} else {
			log.Println("Health check successful")
		}
		time.Sleep(time.Hour * 12)
	}
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, nil)
}

func main() {

	portStr := os.Getenv("PORT")
	port := 8080
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		} else {
			fmt.Println("Invalid PORT value, using default port 8080")
		}
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.Use(LoggerMiddleware())
	r.GET("/health", healthHandler)
	// Define the /new endpoint
	r.POST("/new", newHandler)

	// Define the /version/{name} endpoint
	r.GET("/version/:name", versionHandler)

	// Define the /download/{name} endpoint
	r.GET("/download/:name", downloadHandler)

	// Define the /list endpoint
	r.GET("/list", listHandler)

	go performHealthCheck()

	addr := fmt.Sprintf(":%d", port)
	go func() {
		if err := r.Run(addr); err != nil {
			log.Fatal(err)
		}
	}()

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sigterm:
		log.Fatal("terminating: by signal")
	}
	log.Fatal("shutting down")
	os.Exit(0)
}
