//go:build !linux

package main

// Linux bo'lmagan platformalarda resurs ko'rsatkichlari hozircha qo'llab-quvvatlanmaydi.
// Stats nol bo'lib qoladi (Supported=false), frontend uni yashiradi.
func startStatsSampler() {}
