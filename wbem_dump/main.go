package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/runner-mei/gowbem"
)

var (
	schema    = flag.String("scheme", "", "The scheme of url, when the value is empty, it is automatically selected according to the value of port: 5988 = http, 5989 = https, the default value is http")
	host      = flag.String("host", "192.168.1.157", "IP address of the host")
	port      = flag.String("port", "0", "The port number of the CIM service on the host, when the value is 0, it is automatically selected according to the value of the schema: http = 5988, https = 5989 ")
	path      = flag.String("path", "/cimom", "CIM service access path")
	namespace = flag.String("namespace", "", "CIM namespace, default value: root/cimv2")
	classname = flag.String("class", "", "CIM's class name")
	onlyclass = flag.Bool("onlyclass", false, "List only class names")

	username     = flag.String("username", "root", "username")
	userpassword = flag.String("password", "root", "user password")
	output       = flag.String("output", "", "The output directory of the result, the default value is the current directory")
	debug        = flag.Bool("debug", true, "Are you debugging?")
)

func createURI() *url.URL {
	return &url.URL{
		Scheme: *schema,
		User:   url.UserPassword(*username, *userpassword),
		Host:   *host + ":" + *port,
		Path:   *path,
	}
}

func main() {
	flag.Usage = func() {
		fmt.Println("Instructions： wbem_dump -host=192.168.1.157 -port=5988 -username=root -password=rootpwd\r\n" +
			"Available options")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *output == "" {
		*output = "./" + *host
	}

	if err := os.MkdirAll(*output, 666); err != nil && !os.IsExist(err) {
		log.Fatalln(err)
	}

	if *debug {
		gowbem.SetDebugProvider(&gowbem.FileDebugProvider{Path: *output})
	}

	if *port == "" || *port == "0" {
		switch *schema {
		case "http":
			*port = "5988"
		case "https":
			*port = "5989"
		case "":
			*schema = "http"
			*port = "5988"
		}
	} else if *schema == "" {
		switch *port {
		case "5988":
			*schema = "http"
		case "5989":
			*schema = "https"
		}
	}

	c, e := gowbem.NewClientCIMXML(createURI(), true)
	if nil != e {
		log.Fatalln("Connection failed，", e)
	}

	if *classname != "" && *namespace != "" {
		instancePaths := make(map[string]error, 1024)
		dumpClass(c, *namespace, *classname, instancePaths)
		return
	}

	var namespaces []string
	timeCtx, _ := context.WithTimeout(context.Background(), 30*time.Second)

	if "" == *namespace {
		var err error
		namespaces, err = c.EnumerateNamespaces(timeCtx, []string{"root/cimv2"}, 10*time.Second, nil)
		if nil != err {
			log.Fatalln("Connection failed，", err)
		}
	} else {
		namespaces = []string{*namespace}
	}

	fmt.Println("Namespace：", namespaces)
	for _, ns := range namespaces {
		fmt.Println("Start processing ", ns)
		dumpNS(c, ns)
	}
	if len(namespaces) == 0 {
		fmt.Println("Export failed！")
		os.Exit(1)
		return
	}
	fmt.Println("Successfully exported！")
}

func dumpNS(c *gowbem.ClientCIMXML, ns string) {
	qualifiers, e := c.EnumerateQualifierTypes(context.Background(), ns)
	if nil != e {
		fmt.Println("Enumerating QualifierType fail，", e)
		// return
	}

	if len(qualifiers) > 0 {
		nsPath := strings.Replace(ns, "/", "#", -1)
		nsPath = strings.Replace(nsPath, "\\", "@", -1)

		/// @begin will Qualifier Write definition to file
		filename := filepath.Join(*output, nsPath, "qa.xml")
		if err := os.MkdirAll(filepath.Join(*output, nsPath), 666); err != nil && !os.IsExist(err) {
			log.Fatalln(err)
		}

		var sb bytes.Buffer
		sb.WriteString(`<?xml version="1.0"?>
<CIM CIMVERSION="2.0" DTDVERSION="2.0">
<DECLARATION>
<DECLGROUP>`)
		for idx := range qualifiers {
			sb.WriteString("\r\n")
			sb.WriteString(`<VALUE.OBJECT>`)
			sb.WriteString("\r\n")

			bs, err := xml.MarshalIndent(qualifiers[idx], "", "  ")
			if err != nil {
				log.Fatalln(err)
			}
			sb.Write(bs)

			sb.WriteString("\r\n")
			sb.WriteString(`</VALUE.OBJECT>`)
			sb.WriteString("\r\n")
		}

		sb.WriteString(`</DECLGROUP>
</DECLARATION>
</CIM>`)

		if err := ioutil.WriteFile(filename, sb.Bytes(), 666); err != nil {
			log.Fatalln(err)
		}
		/// @end
	}

	classNames, e := c.EnumerateClassNames(context.Background(), ns, "", true)
	if nil != e {
		if !gowbem.IsErrNotSupported(e) && !gowbem.IsEmptyResults(e) {
			fmt.Println("Failed to enumerate class name，", e)
		}
		return
	}
	if 0 == len(classNames) {
		fmt.Println("No class definition？，")
		return
	}

	if *onlyclass {
		fmt.Println("Command space ", ns, "Under：")
		for _, className := range classNames {
			fmt.Println(className)
		}
		return
	}

	instancePaths := make(map[string]error, 1024)
	fmt.Println("Command space ", ns, "Under：", classNames)

	for _, className := range classNames {
		timeCtx, _ := context.WithTimeout(context.Background(), 30*time.Second)
		class, err := c.GetClass(timeCtx, ns, className, true, true, true, nil)
		if err != nil {
			fmt.Println("Failed to take several names - ", err)
		}

		nsPath := strings.Replace(ns, "/", "#", -1)
		nsPath = strings.Replace(nsPath, "\\", "@", -1)

		/// @begin Write class definition to file
		filename := filepath.Join(*output, nsPath, className+".xml")
		if err := os.MkdirAll(filepath.Join(*output, nsPath), 666); err != nil && !os.IsExist(err) {
			log.Fatalln(err)
		}
		if err := ioutil.WriteFile(filename, []byte(class), 666); err != nil {
			log.Fatalln(err)
		}
		/// @end

		dumpClass(c, ns, className, instancePaths)
	}

	for key, err := range instancePaths {
		if err != nil {
			fmt.Println(key, "Get failed:", err)
		}
	}
}

func dumpClass(c *gowbem.ClientCIMXML, ns, className string, instancePaths map[string]error) {
	nsPath := strings.Replace(ns, "/", "#", -1)
	nsPath = strings.Replace(nsPath, "\\", "@", -1)

	timeCtx, _ := context.WithTimeout(context.Background(), 30*time.Second)
	instanceNames, err := c.EnumerateInstanceNames(timeCtx, ns, className)
	if err != nil {

		/// @begin Write class definition to file
		classPath := filepath.Join(*output, nsPath)
		if e := os.MkdirAll(classPath, 666); e != nil && !os.IsExist(e) {
			log.Fatalln(e)
		}
		if e := ioutil.WriteFile(filepath.Join(classPath, "error.txt"), []byte(err.Error()), 666); e != nil {
			log.Fatalln(e)
		}

		fmt.Println(className, 0, err)

		if !gowbem.IsErrNotSupported(err) && !gowbem.IsEmptyResults(err) {
			fmt.Println(fmt.Sprintf("%T %v", err, err))
		}
		return
	}
	fmt.Println(className, len(instanceNames))

	if len(instanceNames) == 0 {
		return
	}

	/// @begin Write class definition to file
	classPath := filepath.Join(*output, nsPath, className)
	if err := os.MkdirAll(classPath, 666); err != nil && !os.IsExist(err) {
		log.Fatalln(err)
	}
	var buf bytes.Buffer
	for _, instanceName := range instanceNames {
		buf.WriteString(instanceName.String())
		buf.WriteString("\r\n")
	}
	if err := ioutil.WriteFile(filepath.Join(classPath, "instances.txt"), buf.Bytes(), 666); err != nil {
		log.Fatalln(err)
	}
	/// @end

	for idx, instanceName := range instanceNames {
		if _, exists := instancePaths[instanceName.String()]; exists {
			continue
		}

		timeCtx, _ := context.WithTimeout(context.Background(), 30*time.Second)
		instance, err := c.GetInstanceByInstanceName(timeCtx, ns, instanceName, false, true, true, nil)
		if err != nil {
			instancePaths[instanceName.String()] = err

			if !gowbem.IsErrNotSupported(err) && !gowbem.IsEmptyResults(err) {
				fmt.Println(fmt.Sprintf("%T %v", err, err))
			}
			continue
		}

		/// @begin Write class definition to file
		bs, err := xml.MarshalIndent(instance, "", "  ")
		if err != nil {
			log.Fatalln(err)
		}

		subclassPath := filepath.Join(*output, nsPath, instanceName.GetClassName())
		if err := os.MkdirAll(subclassPath, 666); err != nil && !os.IsExist(err) {
			log.Fatalln(err)
		}

		if err := ioutil.WriteFile(filepath.Join(subclassPath, "instance_"+strconv.Itoa(idx)+".xml"), bs, 666); err != nil {
			log.Fatalln(err)
		}
		/// @end

		instancePaths[instanceName.String()] = nil

		// fmt.Println()
		// fmt.Println()
		// fmt.Println(instanceName.String())
		//for _, k := range instance.GetProperties() {
		//	fmt.Println(k.GetName(), k.GetValue())
		//}
	}
}
