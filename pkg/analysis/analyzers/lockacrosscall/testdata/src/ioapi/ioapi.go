package ioapi

func Call() {}

func CallFor[T any]() {}

func Other() {}

type Client struct{}

func (Client) Call() {}
