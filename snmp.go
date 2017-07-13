package main

import (
	"github.com/soniah/gosnmp"
	"net"
	"time"
)

const (
	snmpTrapOid     = "1.3.6.1.6.3.1.1.4.1.0"
	basePeerPairOid = "1.3.6.1.4.1.50255.1."
)

type Snmper struct {
	*gosnmp.GoSNMP
}

func (s *Snmper) Start(hostname string) error {
	log.Info("Sending SNMP Start with hostname", hostname)

	pdus := []gosnmp.SnmpPDU{
		gosnmp.SnmpPDU{
			Name:  snmpTrapOid,
			Type:  gosnmp.ObjectIdentifier,
			Value: basePeerPairOid + "0",
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "0.0",
			Type:  gosnmp.OctetString,
			Value: hostname,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "0.1",
			Type:  gosnmp.TimeTicks, //gosnmp.OctetString,
			Value: uint32(0),
		},
	}

	_, err := s.SendTrap(pdus)

	return err
}

func (s *Snmper) ConfigError(hostname, configserver, failuretype string) error {
	log.Error("Sending SNMP ConfigError with hostname", hostname, configserver, failuretype)

	pdus := []gosnmp.SnmpPDU{
		gosnmp.SnmpPDU{
			Name:  snmpTrapOid,
			Type:  gosnmp.ObjectIdentifier,
			Value: basePeerPairOid + "3",
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "3.0",
			Type:  gosnmp.OctetString,
			Value: hostname,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "3.1",
			Type:  gosnmp.OctetString,
			Value: configserver,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "3.2",
			Type:  gosnmp.OctetString,
			Value: failuretype,
		},
	}

	_, err := s.SendTrap(pdus)
	return err
}

func (s *Snmper) ConfigRead(hostname, configserver string) error {
	log.Info("Sending SNMP ConfigRead with hostname", hostname, configserver)

	pdus := []gosnmp.SnmpPDU{
		gosnmp.SnmpPDU{
			Name:  snmpTrapOid,
			Type:  gosnmp.ObjectIdentifier,
			Value: basePeerPairOid + "4",
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "4.0",
			Type:  gosnmp.OctetString,
			Value: hostname,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "4.1",
			Type:  gosnmp.OctetString,
			Value: configserver,
		},
	}

	_, err := s.SendTrap(pdus)
	if err != nil {
		log.Error("ConfigRead failed to send with error", err)
	}
	return err
}

func (s *Snmper) UnknownPacket(hostname, fromaddress, failuretype string) error {
	log.Info("Sending SNMP UnknownPacket with hostname", hostname, fromaddress, failuretype)

	pdus := []gosnmp.SnmpPDU{
		gosnmp.SnmpPDU{
			Name:  snmpTrapOid,
			Type:  gosnmp.ObjectIdentifier,
			Value: basePeerPairOid + "5",
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "5.0",
			Type:  gosnmp.OctetString,
			Value: hostname,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "5.1",
			Type:  gosnmp.OctetString,
			Value: fromaddress,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "5.2",
			Type:  gosnmp.OctetString,
			Value: failuretype,
		},
	}

	_, err := s.SendTrap(pdus)
	return err
}

func (s *Snmper) ConnectionDown(hostname, fromaddress, toaddress, description, failuretype string) error {
	log.Error("Sending SNMP ConnectionDown", hostname, fromaddress, toaddress, description, failuretype)
	//  hostname, localIPAddress, localPort, remoteIPAddress, remotePort, connectionDescription, failureType

	localIPAddress, localPort, err := net.SplitHostPort(fromaddress)
	if err != nil {
		log.Error("Failed to parse fromaddress, unable to send SNMP ConnectionDown message", fromaddress, err)
		return err
	}
	remoteIPAddress, remotePort, err := net.SplitHostPort(toaddress)
	if err != nil {
		log.Error("Failed to parse fromaddress, unable to send SNMP ConnectionDown message", toaddress, err)
		return err
	}

	pdus := []gosnmp.SnmpPDU{
		gosnmp.SnmpPDU{
			Name:  snmpTrapOid,
			Type:  gosnmp.ObjectIdentifier,
			Value: basePeerPairOid + "1",
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "1.0",
			Type:  gosnmp.OctetString,
			Value: hostname,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "1.1",
			Type:  gosnmp.OctetString,
			Value: localIPAddress,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "1.2",
			Type:  gosnmp.OctetString,
			Value: localPort,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "1.3",
			Type:  gosnmp.OctetString,
			Value: remoteIPAddress,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "1.4",
			Type:  gosnmp.OctetString,
			Value: remotePort,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "1.5",
			Type:  gosnmp.OctetString,
			Value: description,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "1.6",
			Type:  gosnmp.OctetString,
			Value: failuretype,
		},
	}

	_, err = s.SendTrap(pdus)
	if err != nil {
		log.Error("ConnectionDown failed to send with error", err)
	}
	return err
}

func (s *Snmper) ConnectionUp(hostname, fromaddress, toaddress, description string) error {
	log.Info("Sending SNMP ConnectionUp", hostname, fromaddress, toaddress, description)
	//  hostname, localIPAddress, localPort, remoteIPAddress, remotePort, connectionDescription

	localIPAddress, localPort, err := net.SplitHostPort(fromaddress)
	if err != nil {
		log.Error("Failed to parse fromaddress, unable to send SNMP ConnectionUp message", fromaddress, err)
		return err
	}
	remoteIPAddress, remotePort, err := net.SplitHostPort(toaddress)
	if err != nil {
		log.Error("Failed to parse fromaddress, unable to send SNMP ConnectionUp message", toaddress, err)
		return err
	}

	pdus := []gosnmp.SnmpPDU{
		gosnmp.SnmpPDU{
			Name:  snmpTrapOid,
			Type:  gosnmp.ObjectIdentifier,
			Value: basePeerPairOid + "2",
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "2.0",
			Type:  gosnmp.OctetString,
			Value: hostname,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "2.1",
			Type:  gosnmp.OctetString,
			Value: localIPAddress,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "2.2",
			Type:  gosnmp.OctetString,
			Value: localPort,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "2.3",
			Type:  gosnmp.OctetString,
			Value: remoteIPAddress,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "2.4",
			Type:  gosnmp.OctetString,
			Value: remotePort,
		},
		gosnmp.SnmpPDU{
			Name:  basePeerPairOid + "2.5",
			Type:  gosnmp.OctetString,
			Value: description,
		},
	}

	_, err = s.SendTrap(pdus)
	if err != nil {
		log.Error("ConnectionUp failed to send with error", err)
	}
	return err
}

func snmpLogInit(snmpServer string, snmpPort uint16, community string) (*Snmper, error) {

	snmp := &gosnmp.GoSNMP{
		Target:    snmpServer,
		Port:      snmpPort,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Duration(2) * time.Second,
		Retries:   3,
		MaxOids:   30,
	}

	err := snmp.Connect()
	if err != nil {
		log.Error("SNMP connect error:", err)
		return nil, err
	}

	snmper := &Snmper{GoSNMP: snmp}
	return snmper, nil
}
