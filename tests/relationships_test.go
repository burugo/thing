package thing_test

import (
	"testing"

	"github.com/burugo/thing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThing_Query_Preload_BelongsTo(t *testing.T) {
	// Set up test DB and mockcache
	db, mockcache, cleanup := setupTestDB(t)
	defer cleanup()

	userThing, err := thing.New[*User](db, mockcache)
	require.NoError(t, err)

	bookThing, err := thing.New[*Book](db, mockcache)
	require.NoError(t, err)

	// Create a test user
	user := &User{
		Name:  "Book Owner",
		Email: "owner@example.com",
	}
	err = userThing.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// Create books belonging to this user
	book := &Book{
		Title:  "User's Book",
		UserID: user.ID,
	}
	err = bookThing.Save(book)
	require.NoError(t, err)
	require.NotZero(t, book.ID)

	// Query for the book with preloaded user
	params := thing.QueryParams{
		Where:    "id = ?",
		Args:     []interface{}{book.ID},
		Preloads: []string{"User"},
	}

	booksResult := bookThing.Query(params)
	// Fetch before len/indexing
	fetchedBooks, fetchErr := booksResult.Fetch(0, 10)
	require.NoError(t, fetchErr)
	require.Len(t, fetchedBooks, 1)

	// Verify the relationship was loaded correctly
	assert.NotNil(t, fetchedBooks[0].User)
	assert.Equal(t, user.ID, fetchedBooks[0].User.ID)
	assert.Equal(t, user.Name, fetchedBooks[0].User.Name)
	assert.Equal(t, user.Email, fetchedBooks[0].User.Email)
}

func TestThing_Query_Preload_HasMany(t *testing.T) {
	// Set up test DB and mockcache
	db, mockcache, cleanup := setupTestDB(t)
	defer cleanup()

	userThing, err := thing.New[*User](db, mockcache)
	require.NoError(t, err)

	bookThing, err := thing.New[*Book](db, mockcache)
	require.NoError(t, err)

	// Create a test user
	user := &User{
		Name:  "Multiple Books Owner",
		Email: "multiple@example.com",
	}
	err = userThing.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// Create multiple books belonging to this user
	books := []*Book{
		{Title: "First Book", UserID: user.ID},
		{Title: "Second Book", UserID: user.ID},
		{Title: "Third Book", UserID: user.ID},
	}

	for _, b := range books {
		err = bookThing.Save(b)
		require.NoError(t, err)
		require.NotZero(t, b.ID)
	}

	// Query for the user
	params := thing.QueryParams{
		Where:    "id = ?",
		Args:     []interface{}{user.ID},
		Preloads: []string{"Books"},
	}

	usersResult := userThing.Query(params)
	// Fetch before len/indexing
	fetchedUsers, fetchErr := usersResult.Fetch(0, 10)
	require.NoError(t, fetchErr)
	require.Len(t, fetchedUsers, 1)

	// Verify the relationship was loaded correctly
	assert.NotNil(t, fetchedUsers[0].Books)
	assert.Len(t, fetchedUsers[0].Books, 3)

	// Verify book properties
	titles := make(map[string]bool)
	for _, b := range fetchedUsers[0].Books {
		assert.Equal(t, user.ID, b.UserID)
		titles[b.Title] = true
	}

	assert.True(t, titles["First Book"])
	assert.True(t, titles["Second Book"])
	assert.True(t, titles["Third Book"])

	// --- New: Chainable Preload ---
	fetchedUsersChained, err := userThing.Where("id = ?", user.ID).Preload("Books").Fetch(0, 10)
	require.NoError(t, err)
	require.Len(t, fetchedUsersChained, 1)
	assert.NotNil(t, fetchedUsersChained[0].Books)
	assert.Len(t, fetchedUsersChained[0].Books, 3)
}

func TestThing_Load_BelongsTo(t *testing.T) {
	// Set up test DB and cache
	db, cache, cleanup := setupTestDB(t)
	defer cleanup()

	userThing, err := thing.New[*User](db, cache)
	require.NoError(t, err)

	bookThing, err := thing.New[*Book](db, cache)
	require.NoError(t, err)

	// Create a test user
	user := &User{
		Name:  "Another Book Owner",
		Email: "another@example.com",
	}
	err = userThing.Save(user)
	require.NoError(t, err)

	// Create a book belonging to this user
	book := &Book{
		Title:  "Another Book",
		UserID: user.ID,
	}
	err = bookThing.Save(book)
	require.NoError(t, err)

	// Fetch the book
	fetchedBook, err := bookThing.ByID(book.ID)
	require.NoError(t, err)

	// Initially the User field should be nil
	assert.Nil(t, fetchedBook.User)

	// Load the belongs-to relationship
	err = bookThing.Load(fetchedBook, "User")
	require.NoError(t, err)

	// Now the User field should be populated
	assert.NotNil(t, fetchedBook.User)
	assert.Equal(t, user.ID, fetchedBook.User.ID)
	assert.Equal(t, user.Name, fetchedBook.User.Name)
	assert.Equal(t, user.Email, fetchedBook.User.Email)
}

func TestThing_Load_HasMany(t *testing.T) {
	// Set up test DB and cache
	db, cache, cleanup := setupTestDB(t)
	defer cleanup()

	userThing, err := thing.New[*User](db, cache)
	require.NoError(t, err)

	bookThing, err := thing.New[*Book](db, cache)
	require.NoError(t, err)

	// Create a test user
	user := &User{
		Name:  "Yet Another Owner",
		Email: "yetanother@example.com",
	}
	err = userThing.Save(user)
	require.NoError(t, err)

	// Create books belonging to this user
	bookTitles := []string{"Book A", "Book B", "Book C"}
	for _, title := range bookTitles {
		book := &Book{
			Title:  title,
			UserID: user.ID,
		}
		err = bookThing.Save(book)
		require.NoError(t, err)
	}

	// Fetch the user
	fetchedUser, err := userThing.ByID(user.ID)
	require.NoError(t, err)

	// Initially the Books field should be empty
	assert.Len(t, fetchedUser.Books, 0)

	// Load the has-many relationship
	err = userThing.Load(fetchedUser, "Books")
	require.NoError(t, err)

	// Now the Books field should be populated
	assert.Len(t, fetchedUser.Books, 3)

	// Check that all books have the correct titles and user ID
	titles := make(map[string]bool)
	for _, book := range fetchedUser.Books {
		assert.Equal(t, user.ID, book.UserID)
		titles[book.Title] = true
	}

	for _, title := range bookTitles {
		assert.True(t, titles[title], "Book with title %s should be loaded", title)
	}
}

func TestThing_Query_Preload_ManyToMany(t *testing.T) {
	db, mockcache, cleanup := setupTestDB(t)
	defer cleanup()

	// Ensure thing is configured before AutoMigrate and NewThing instances
	require.NoError(t, thing.Configure(db, mockcache), "thing.Configure should not fail")

	studentThing, err := thing.New[*Student](db, mockcache)
	require.NoError(t, err)
	courseThing, err := thing.New[*Course](db, mockcache)
	require.NoError(t, err)
	studentCourseThing, err := thing.New[*StudentCourse](db, mockcache)
	require.NoError(t, err)

	// AutoMigrate Student, Course, and StudentCourse models
	err = thing.AutoMigrate(&Student{}, &Course{}, &StudentCourse{})
	require.NoError(t, err)

	// Create students
	student1 := &Student{Name: "Alice"}
	student2 := &Student{Name: "Bob"}
	err = studentThing.Save(student1)
	require.NoError(t, err)
	err = studentThing.Save(student2)
	require.NoError(t, err)

	// Create courses
	course1 := &Course{Title: "Math 101"}
	course2 := &Course{Title: "History 202"}
	course3 := &Course{Title: "Physics 303"}
	err = courseThing.Save(course1)
	require.NoError(t, err)
	err = courseThing.Save(course2)
	require.NoError(t, err)
	err = courseThing.Save(course3)
	require.NoError(t, err)

	// Create associations in join table
	// Alice takes Math 101 and History 202
	// Bob takes History 202 and Physics 303
	associations := []struct{ studentID, courseID int64 }{
		{student1.ID, course1.ID},
		{student1.ID, course2.ID},
		{student2.ID, course2.ID},
		{student2.ID, course3.ID},
	}
	for _, assoc := range associations {
		err = studentCourseThing.Save(&StudentCourse{StudentID: assoc.studentID, CourseID: assoc.courseID})
		require.NoError(t, err)
	}

	// Test 1: Query student and preload courses
	paramsStudent := thing.QueryParams{
		Where:    "id = ?",
		Args:     []interface{}{student1.ID},
		Preloads: []string{"Courses"},
	}
	fetchedStudents, fetchErr := studentThing.Query(paramsStudent).Fetch(0, 1)
	require.NoError(t, fetchErr)
	require.Len(t, fetchedStudents, 1)
	require.NotNil(t, fetchedStudents[0].Courses)
	assert.Len(t, fetchedStudents[0].Courses, 2)

	student1CourseTitles := make(map[string]bool)
	for _, c := range fetchedStudents[0].Courses {
		student1CourseTitles[c.Title] = true
	}
	assert.True(t, student1CourseTitles["Math 101"])
	assert.True(t, student1CourseTitles["History 202"])

	// Test 2: Query course and preload students
	paramsCourse := thing.QueryParams{
		Where:    "id = ?",
		Args:     []interface{}{course2.ID},
		Preloads: []string{"Students"},
	}
	fetchedCourses, fetchErr := courseThing.Query(paramsCourse).Fetch(0, 1)
	require.NoError(t, fetchErr)
	require.Len(t, fetchedCourses, 1)
	require.NotNil(t, fetchedCourses[0].Students)
	assert.Len(t, fetchedCourses[0].Students, 2)

	course2StudentNames := make(map[string]bool)
	for _, s := range fetchedCourses[0].Students {
		course2StudentNames[s.Name] = true
	}
	assert.True(t, course2StudentNames["Alice"])
	assert.True(t, course2StudentNames["Bob"])
}

func TestThing_Load_ManyToMany(t *testing.T) {
	db, mockcache, cleanup := setupTestDB(t)
	defer cleanup()

	// Ensure thing is configured before AutoMigrate and NewThing instances
	require.NoError(t, thing.Configure(db, mockcache), "thing.Configure should not fail")

	studentThing, err := thing.New[*Student](db, mockcache)
	require.NoError(t, err)
	courseThing, err := thing.New[*Course](db, mockcache)
	require.NoError(t, err)
	studentCourseThing, err := thing.New[*StudentCourse](db, mockcache)
	require.NoError(t, err)

	// AutoMigrate Student, Course, and StudentCourse models
	err = thing.AutoMigrate(&Student{}, &Course{}, &StudentCourse{})
	require.NoError(t, err)

	// Create students
	student1 := &Student{Name: "Charlie"}
	err = studentThing.Save(student1)
	require.NoError(t, err)

	// Create courses
	course1 := &Course{Title: "Chemistry 101"}
	course2 := &Course{Title: "Literature 202"}
	err = courseThing.Save(course1)
	require.NoError(t, err)
	err = courseThing.Save(course2)
	require.NoError(t, err)

	// Create associations in join table
	// Charlie takes Chemistry 101 and Literature 202
	associations := []struct{ studentID, courseID int64 }{
		{student1.ID, course1.ID},
		{student1.ID, course2.ID},
	}
	for _, assoc := range associations {
		err = studentCourseThing.Save(&StudentCourse{StudentID: assoc.studentID, CourseID: assoc.courseID})
		require.NoError(t, err)
	}

	// Test 1: Load courses for a student
	fetchedStudent, err := studentThing.ByID(student1.ID)
	require.NoError(t, err)
	assert.Len(t, fetchedStudent.Courses, 0) // Should be empty before load

	err = studentThing.Load(fetchedStudent, "Courses")
	require.NoError(t, err)
	assert.Len(t, fetchedStudent.Courses, 2)
	studentCourseTitles := make(map[string]bool)
	for _, c := range fetchedStudent.Courses {
		studentCourseTitles[c.Title] = true
	}
	assert.True(t, studentCourseTitles["Chemistry 101"])
	assert.True(t, studentCourseTitles["Literature 202"])

	// Test 2: Load students for a course
	fetchedCourse, err := courseThing.ByID(course1.ID)
	require.NoError(t, err)
	assert.Len(t, fetchedCourse.Students, 0) // Should be empty before load

	err = courseThing.Load(fetchedCourse, "Students")
	require.NoError(t, err)
	assert.Len(t, fetchedCourse.Students, 1)
	assert.Equal(t, "Charlie", fetchedCourse.Students[0].Name)
}
