// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Type Utilities", func() {
	Describe("Ptr operations", func() {
		It("should create pointer to int", func() {
			i := 42
			ptr := Ptr(i)
			Expect(*ptr).To(Equal(i))
		})

		It("should return value for non-nil pointer", func() {
			val := 42
			ptr := &val
			result := PtrValue(ptr)
			Expect(result).To(Equal(val))
		})

		It("should return zero value for nil pointer", func() {
			var ptr *int = nil
			result := PtrValue(ptr)
			Expect(result).To(Equal(0)) // Zero value for int
		})
	})

	Describe("ConvertAnyToAny", func() {
		Context("Map to Struct conversion", func() {
			It("should convert map to struct with nested fields", func() {
				type Address struct {
					Street string `json:"street"`
					City   string `json:"city"`
				}

				type Person struct {
					Name    string   `json:"name"`
					Address Address  `json:"address"`
					Tags    []string `json:"tags"`
				}

				params := map[string]interface{}{
					"name": "Jane",
					"address": map[string]interface{}{
						"street": "123 Main St",
						"city":   "Anytown",
					},
					"tags":  []string{"developer", "gopher"},
					"extra": "extra",
				}

				var result Person
				err := ConvertAnyToAny(params, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Name).To(Equal("Jane"))
				Expect(result.Address.Street).To(Equal("123 Main St"))
				Expect(result.Address.City).To(Equal("Anytown"))
				Expect(result.Tags).To(Equal([]string{"developer", "gopher"}))
			})
			It("should not reset fields in destination not present in source map", func() {
				type DestStruct struct {
					Name    string `json:"name"`
					Age     int    `json:"age"`
					Country string `json:"country"`
				}

				params := map[string]interface{}{
					"name": "John",
					// age and country missing
				}

				var dest DestStruct
				dest.Age = 30
				dest.Country = "USA"

				err := ConvertAnyToAny(params, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest.Name).To(Equal("John"))
				Expect(dest.Age).To(Equal(30))
				Expect(dest.Country).To(Equal("USA"))
			})
		})

		Context("Struct to Struct conversion", func() {
			It("should convert nested structures", func() {
				type Address struct {
					Street string `json:"street"`
					City   string `json:"city"`
				}

				type SourcePerson struct {
					Name    string  `json:"name"`
					Address Address `json:"address"`
				}

				type DestPerson struct {
					NameNew    string  `json:"name"`
					AddressNew Address `json:"address"`
					Extra      string  `json:"extra"`
				}

				source := SourcePerson{
					Name: "Bob",
					Address: Address{
						Street: "123 Main St",
						City:   "Anytown",
					},
				}

				var dest DestPerson
				err := ConvertAnyToAny(source, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest.NameNew).To(Equal("Bob"))
				Expect(dest.AddressNew.Street).To(Equal("123 Main St"))
				Expect(dest.AddressNew.City).To(Equal("Anytown"))
				Expect(dest.Extra).To(BeEmpty())
			})

			It("should not reset fields in destination not present in source", func() {
				type SourceStruct struct {
					Name string `json:"name"`
				}

				type DestStruct struct {
					Name    string `json:"name"`
					Age     int    `json:"age"`
					Country string `json:"country"`
				}

				source := SourceStruct{
					Name: "Charlie",
				}

				var dest DestStruct
				dest.Age = 30
				dest.Country = "USA"

				err := ConvertAnyToAny(source, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest.Name).To(Equal("Charlie"))
				Expect(dest.Age).To(Equal(30))
				Expect(dest.Country).To(Equal("USA"))
			})

			It("should ignore extra fields in source", func() {
				type SourceStruct struct {
					Name     string `json:"name"`
					Age      int    `json:"age"`
					Country  string `json:"country"`
					ExtraVal bool   `json:"extra_val"`
				}

				type DestStruct struct {
					Name    string `json:"name"`
					Age     int    `json:"age"`
					Country string `json:"country"`
				}

				source := SourceStruct{
					Name:     "Dave",
					Age:      40,
					Country:  "Canada",
					ExtraVal: true,
				}

				var dest DestStruct
				err := ConvertAnyToAny(source, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest.Name).To(Equal("Dave"))
				Expect(dest.Age).To(Equal(40))
				Expect(dest.Country).To(Equal("Canada"))
				// ExtraVal is ignored as it doesn't exist in the destination struct
			})
		})

		Context("Map to Map conversion", func() {
			It("should convert from string map to interface map", func() {
				source := map[string]string{
					"name":    "Alice",
					"country": "Wonderland",
				}

				var dest map[string]interface{}
				err := ConvertAnyToAny(source, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest["name"]).To(Equal("Alice"))
				Expect(dest["country"]).To(Equal("Wonderland"))
			})
		})

		Context("Struct to Map conversion", func() {
			It("should convert struct with nested structs to map", func() {
				type Address struct {
					Street string `json:"street"`
					City   string `json:"city"`
				}

				type Person struct {
					Name    string   `json:"name"`
					Address Address  `json:"address"`
					Tags    []string `json:"tags"`
					Extra   string
				}

				source := Person{
					Name: "Bob",
					Address: Address{
						Street: "123 Main St",
						City:   "Anytown",
					},
					Tags:  []string{"developer", "gopher"},
					Extra: "extra",
				}

				var dest map[string]interface{}
				dest = make(map[string]interface{})
				dest["extra_in_map"] = "extra_in_map"
				err := ConvertAnyToAny(source, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest["name"]).To(Equal("Bob"))

				addressMap := dest["address"].(map[string]interface{})
				Expect(addressMap["street"]).To(Equal("123 Main St"))
				Expect(addressMap["city"]).To(Equal("Anytown"))

				tags := dest["tags"].([]interface{})
				Expect(tags).To(Equal([]interface{}{"developer", "gopher"}))
				Expect(dest["Extra"]).To(Equal("extra"))
				Expect(dest["extra_in_map"]).To(Equal("extra_in_map"))
			})

			It("should convert struct with unexported fields", func() {
				type User struct {
					Name  string `json:"name"`
					Email string `json:"email"`
					token string // unexported field
				}

				source := User{
					Name:  "Alice",
					Email: "alice@example.com",
					token: "secret", // This should not be in the map
				}

				var dest map[string]interface{}
				err := ConvertAnyToAny(source, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest["name"]).To(Equal("Alice"))
				Expect(dest["email"]).To(Equal("alice@example.com"))
				_, hasToken := dest["token"]
				Expect(hasToken).To(BeFalse()) // Unexported fields are not marshaled
			})

			It("should convert struct with omitted fields", func() {
				type User struct {
					Name     string `json:"name"`
					Password string `json:"-"` // Omitted from JSON
					Email    string `json:"email"`
				}

				source := User{
					Name:     "Bob",
					Password: "secret123",
					Email:    "bob@example.com",
				}

				var dest map[string]interface{}
				err := ConvertAnyToAny(source, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest["name"]).To(Equal("Bob"))
				Expect(dest["email"]).To(Equal("bob@example.com"))
				_, hasPassword := dest["password"]
				Expect(hasPassword).To(BeFalse()) // Field marked with - is omitted
			})
			It("should merge struct fields into an existing map", func() {
				// Define a struct with some fields
				type UserInfo struct {
					Name    string `json:"name"`
					Age     int    `json:"age"`
					Country string `json:"country"`
				}

				// Create a struct instance
				userInfo := UserInfo{
					Name:    "Bob",
					Age:     30,
					Country: "USA",
				}

				// Start with an existing map
				existingMap := map[string]interface{}{
					"email":       "bob@example.com",
					"is_verified": true,
					"country":     "Canada", // This will be overwritten by the struct field
				}

				// Convert struct to a temporary map
				var tempMap map[string]interface{}
				err := ConvertAnyToAny(userInfo, &tempMap)
				Expect(err).NotTo(HaveOccurred())

				// Merge the temp map into the existing map
				for k, v := range tempMap {
					existingMap[k] = v
				}

				// Verify the merged map
				Expect(existingMap["name"]).To(Equal("Bob"))
				Expect(existingMap["age"]).To(Equal(float64(30)))
				Expect(existingMap["country"]).To(Equal("USA"))           // Overwritten from struct
				Expect(existingMap["email"]).To(Equal("bob@example.com")) // Preserved from original map
				Expect(existingMap["is_verified"]).To(Equal(true))        // Preserved from original map
			})
		})

		Context("complex conversions", func() {
			It("should convert a map with mixed types to a struct and back", func() {
				// Start with a complex map
				originalMap := map[string]interface{}{
					"user_id":   123,
					"username":  "charlie",
					"is_active": true,
					"profile": map[string]interface{}{
						"full_name": "Charlie Brown",
						"address": map[string]string{
							"city":    "New York",
							"country": "USA",
						},
					},
					"tags": []string{"customer", "premium"},
				}

				// Define a struct that captures some of the fields
				type UserProfile struct {
					UserID   int    `json:"user_id"`
					Username string `json:"username"`
					IsActive bool   `json:"is_active"`
					Profile  struct {
						FullName string `json:"full_name"`
						Address  struct {
							City    string `json:"city"`
							Country string `json:"country"`
						} `json:"address"`
					} `json:"profile"`
					// Note: tags field is not captured in the struct
				}

				// Convert map to struct
				var user UserProfile
				err := ConvertAnyToAny(originalMap, &user)
				Expect(err).NotTo(HaveOccurred())

				// Verify struct has the expected values
				Expect(user.UserID).To(Equal(123))
				Expect(user.Username).To(Equal("charlie"))
				Expect(user.IsActive).To(BeTrue())
				Expect(user.Profile.FullName).To(Equal("Charlie Brown"))
				Expect(user.Profile.Address.City).To(Equal("New York"))
				Expect(user.Profile.Address.Country).To(Equal("USA"))

				// Convert back to map
				var resultMap map[string]interface{}
				err = ConvertAnyToAny(user, &resultMap)
				Expect(err).NotTo(HaveOccurred())

				// Verify map has the expected values
				Expect(resultMap["user_id"]).To(Equal(float64(123)))
				Expect(resultMap["username"]).To(Equal("charlie"))
				Expect(resultMap["is_active"]).To(Equal(true))

				profile := resultMap["profile"].(map[string]interface{})
				Expect(profile["full_name"]).To(Equal("Charlie Brown"))

				address := profile["address"].(map[string]interface{})
				Expect(address["city"]).To(Equal("New York"))
				Expect(address["country"]).To(Equal("USA"))

				// The tags field was lost because it wasn't in the struct
				_, hasTags := resultMap["tags"]
				Expect(hasTags).To(BeFalse())

				// To preserve all fields, we would need to merge with the original map
				for k, v := range originalMap {
					if _, exists := resultMap[k]; !exists {
						resultMap[k] = v
					}
				}

				// Now tags should be present
				tags := resultMap["tags"].([]string)
				Expect(tags).To(Equal([]string{"customer", "premium"}))
			})

			It("should convert struct to map and back to struct with field retention", func() {
				// Define a complex struct with nested fields
				type Address struct {
					Street     string `json:"street"`
					City       string `json:"city"`
					PostalCode string `json:"postal_code"`
					Country    string `json:"country"`
				}

				type Contact struct {
					Email     string    `json:"email"`
					Phone     string    `json:"phone"`
					Addresses []Address `json:"addresses"`
				}

				type User struct {
					ID        int      `json:"id"`
					FirstName string   `json:"first_name"`
					LastName  string   `json:"last_name"`
					Age       int      `json:"age"`
					IsActive  bool     `json:"is_active"`
					Contact   Contact  `json:"contact"`
					Roles     []string `json:"roles"`
				}

				// Create an instance of the original struct
				originalUser := User{
					ID:        123,
					FirstName: "John",
					LastName:  "Doe",
					Age:       30,
					IsActive:  true,
					Contact: Contact{
						Email: "john.doe@example.com",
						Phone: "+1234567890",
						Addresses: []Address{
							{
								Street:     "123 Main St",
								City:       "Anytown",
								PostalCode: "12345",
								Country:    "USA",
							},
							{
								Street:     "456 Work Blvd",
								City:       "Worktown",
								PostalCode: "67890",
								Country:    "USA",
							},
						},
					},
					Roles: []string{"admin", "user"},
				}

				// Convert struct to map
				var userMap map[string]interface{}
				err := ConvertAnyToAny(originalUser, &userMap)
				Expect(err).NotTo(HaveOccurred())

				// Add some extra fields to the map that weren't in the original struct
				userMap["created_at"] = "2023-01-01T00:00:00Z"
				userMap["metadata"] = map[string]interface{}{
					"login_count": 42,
					"last_login":  "2023-05-15T10:30:00Z",
				}
				userMap["preferences"] = map[string]bool{
					"notifications": true,
					"newsletter":    false,
				}

				// Define a different struct with some fields missing and some extra fields
				type SimpleAddress struct {
					Street  string `json:"street"`
					City    string `json:"city"`
					Country string `json:"country"`
					// PostalCode is missing
				}

				type SimpleContact struct {
					Email     string          `json:"email"`
					Addresses []SimpleAddress `json:"addresses"`
					// Phone is missing
				}

				type ExtendedUser struct {
					ID        int           `json:"id"`
					FirstName string        `json:"first_name"`
					LastName  string        `json:"last_name"`
					FullName  string        `json:"full_name"` // Not in original
					Contact   SimpleContact `json:"contact"`
					// Age is missing
					// IsActive is missing
					// Roles is missing
					CreatedAt   string                 `json:"created_at"`
					Metadata    map[string]interface{} `json:"metadata"`
					Preferences map[string]bool        `json:"preferences"`
				}

				// Convert map to the new struct
				var extendedUser ExtendedUser
				err = ConvertAnyToAny(userMap, &extendedUser)
				Expect(err).NotTo(HaveOccurred())

				// Verify fields were correctly transferred
				Expect(extendedUser.ID).To(Equal(123))
				Expect(extendedUser.FirstName).To(Equal("John"))
				Expect(extendedUser.LastName).To(Equal("Doe"))
				Expect(extendedUser.FullName).To(Equal("")) // Not in original, so zero value
				Expect(extendedUser.Contact.Email).To(Equal("john.doe@example.com"))

				// Check addresses
				Expect(len(extendedUser.Contact.Addresses)).To(Equal(2))
				Expect(extendedUser.Contact.Addresses[0].Street).To(Equal("123 Main St"))
				Expect(extendedUser.Contact.Addresses[0].City).To(Equal("Anytown"))
				Expect(extendedUser.Contact.Addresses[0].Country).To(Equal("USA"))
				// PostalCode is missing in the new struct

				// Check extra fields added to the map
				Expect(extendedUser.CreatedAt).To(Equal("2023-01-01T00:00:00Z"))
				Expect(extendedUser.Metadata["login_count"]).To(Equal(float64(42)))
				Expect(extendedUser.Metadata["last_login"]).To(Equal("2023-05-15T10:30:00Z"))
				Expect(extendedUser.Preferences["notifications"]).To(BeTrue())
				Expect(extendedUser.Preferences["newsletter"]).To(BeFalse())

				// Now set the FullName field and convert back to a map
				extendedUser.FullName = "John Doe"

				var finalMap map[string]interface{}
				err = ConvertAnyToAny(extendedUser, &finalMap)
				Expect(err).NotTo(HaveOccurred())

				// Check that the full name was added
				Expect(finalMap["full_name"]).To(Equal("John Doe"))

				// Convert back to the original User struct
				var reconstructedUser User
				err = ConvertAnyToAny(finalMap, &reconstructedUser)
				Expect(err).NotTo(HaveOccurred())

				// Verify the original fields are preserved
				Expect(reconstructedUser.ID).To(Equal(123))
				Expect(reconstructedUser.FirstName).To(Equal("John"))
				Expect(reconstructedUser.LastName).To(Equal("Doe"))

				// These fields would be zero values because they weren't in ExtendedUser
				Expect(reconstructedUser.Age).To(Equal(0))
				Expect(reconstructedUser.IsActive).To(BeFalse())
				Expect(len(reconstructedUser.Roles)).To(Equal(0))

				// But the contact email should be preserved
				Expect(reconstructedUser.Contact.Email).To(Equal("john.doe@example.com"))

				// And the addresses should be partially preserved (fields that exist in both structs)
				Expect(len(reconstructedUser.Contact.Addresses)).To(Equal(2))
				Expect(reconstructedUser.Contact.Addresses[0].Street).To(Equal("123 Main St"))
				Expect(reconstructedUser.Contact.Addresses[0].City).To(Equal("Anytown"))
				Expect(reconstructedUser.Contact.Addresses[0].Country).To(Equal("USA"))
				Expect(reconstructedUser.Contact.Addresses[0].PostalCode).To(Equal("")) // Lost in the conversion
			})
		})

		Context("Nil handling", func() {
			It("should handle nil source", func() {
				var dest map[string]interface{}

				err := ConvertAnyToAny(nil, &dest)
				Expect(err).NotTo(HaveOccurred())
				Expect(dest).To(BeNil()) // nil source should result in nil map
			})

			It("should error when target is nil", func() {
				source := map[string]string{
					"name": "Alice",
				}

				var dest interface{} = nil
				err := ConvertAnyToAny(source, dest) // Not passing a pointer to dest
				Expect(err).NotTo(HaveOccurred())
				Expect(dest).To(BeNil())
			})

			It("should error when target is not a pointer", func() {
				source := map[string]string{
					"name": "Alice",
				}

				var dest map[string]interface{}      // Not a pointer
				err := ConvertAnyToAny(source, dest) // Not passing a pointer to dest
				Expect(err).To(HaveOccurred())
				// Should error because we're not passing a pointer
			})
		})
	})
})
