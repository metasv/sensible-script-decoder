package script

import (
	"bytes"
	"encoding/binary"
)

var empty = make([]byte, 1)

func hasSensibleFlag(scriptPk []byte) bool {
	return bytes.HasSuffix(scriptPk, []byte("sensible")) || bytes.HasSuffix(scriptPk, []byte("oraclesv"))
}

func DecodeSensibleTxo(scriptPk []byte, txo *TxoData) bool {
	scriptLen := len(scriptPk)
	if scriptLen < 1024 {
		return false
	}

	ret := false
	if hasSensibleFlag(scriptPk) {
		protoTypeOffset := scriptLen - 8 - 4
		if scriptPk[protoTypeOffset] == 1 { // PROTO_TYPE == 1
			ret = decodeFT(scriptLen, scriptPk, txo)
		} else if scriptPk[protoTypeOffset] == 2 { // PROTO_TYPE == 2
			ret = decodeUnique(scriptLen, scriptPk, txo)
		}
	} else if scriptPk[scriptLen-1] < 2 && scriptPk[scriptLen-37-1] == 37 && scriptPk[scriptLen-37-1-40-1] == 40 && scriptPk[scriptLen-37-1-40-1-1] == OP_RETURN {
		ret = decodeNFTIssue(scriptLen, scriptPk, txo)
	} else if scriptPk[scriptLen-1] == 1 && scriptPk[scriptLen-61-1] == 61 && scriptPk[scriptLen-61-1-40-1] == 40 && scriptPk[scriptLen-61-1-40-1-1] == OP_RETURN {
		ret = decodeNFTTransfer(scriptLen, scriptPk, txo)
	}

	return ret
}

func decodeFT(scriptLen int, scriptPk []byte, txo *TxoData) bool {
	dataLen := 0
	genesisIdLen := 0
	sensibleIdLen := 0
	useTokenIdHash := false
	// v5
	// <type specific data> + <proto header>
	// <proto header> = <type(4 bytes)> + <'sensible'(8 bytes)>
	// <token type specific data> = <token_name (20 bytes)> + <token_symbol (10 bytes)> + <is_genesis(1 byte)> + <decimailNum(1 byte)> + <address(20 bytes)> + <token amount(8 bytes)> + <genesisHash(20 bytes)> + <rabinPubKeyHashArrayHash(20 bytes)> + <genesisId(36 bytes)>

	// v1 ~ v4
	// <type specific data> + <proto header>
	// <proto header> = <type(4 bytes)> + <'sensible'(8 bytes)>
	// <token type specific data> = <token_name (20 bytes)> + <token_symbol (10 bytes)> + <is_genesis(1 byte)> + <decimailNum(1 byte)> + <address(20 bytes)> + <token amount(8 bytes)> + <genesisId(x bytes)>

	if scriptPk[scriptLen-72-76-1-1] == 0x4c && scriptPk[scriptLen-72-76-1] == 148 {
		// ft v5
		genesisIdLen = 76
		sensibleIdLen = 36
		dataLen = 1 + 1 + 1 + 72 + genesisIdLen // opreturn + 0x4c + pushdata + data + genesisId
		useTokenIdHash = true
	} else if scriptPk[scriptLen-72-36-1-1] == 0x4c && scriptPk[scriptLen-72-36-1] == 108 {
		// ft v4
		genesisIdLen = 36
		dataLen = 1 + 1 + 1 + 72 + genesisIdLen
	} else if scriptPk[scriptLen-72-20-1-1] == 0x4c && scriptPk[scriptLen-72-20-1] == 92 {
		// ft v3
		genesisIdLen = 20
		dataLen = 1 + 1 + 1 + 72 + genesisIdLen
	} else if scriptPk[scriptLen-50-36-1-1] == 0x4c && scriptPk[scriptLen-50-36-1] == 86 {
		// ft v2
		genesisIdLen = 36
		dataLen = 1 + 1 + 1 + 50 + genesisIdLen
	} else if scriptPk[scriptLen-92-20-1-1] == 0x4c && scriptPk[scriptLen-92-20-1] == 112 {
		// ft v1
		genesisIdLen = 20
		dataLen = 1 + 1 + 1 + 92 + genesisIdLen
	} else {
		// error ft
		return false
	}

	protoTypeOffset := scriptLen - 8 - 4
	sensibleOffset := protoTypeOffset - sensibleIdLen
	genesisOffset := protoTypeOffset - genesisIdLen

	amountOffset := genesisOffset - 8
	addressOffset := amountOffset - 20
	decimalOffset := addressOffset - 1
	symbolOffset := decimalOffset - 1 - 10
	nameOffset := symbolOffset - 20

	txo.CodeType = CodeType_FT
	txo.Decimal = uint64(scriptPk[decimalOffset])
	txo.Symbol = string(bytes.TrimRight(scriptPk[symbolOffset:symbolOffset+10], "\x00"))
	txo.Name = string(bytes.TrimRight(scriptPk[nameOffset:nameOffset+20], "\x00"))

	txo.Amount = binary.LittleEndian.Uint64(scriptPk[amountOffset : amountOffset+8])

	txo.AddressPkh = make([]byte, 20)
	copy(txo.AddressPkh, scriptPk[addressOffset:addressOffset+20])

	txo.CodeHash = GetHash160(scriptPk[:scriptLen-dataLen])
	if useTokenIdHash {
		// GenesisId is tokenIdHash
		txo.GenesisId = GetHash160(scriptPk[genesisOffset : genesisOffset+genesisIdLen])

		txo.SensibleId = make([]byte, sensibleIdLen)
		copy(txo.SensibleId, scriptPk[sensibleOffset:sensibleOffset+sensibleIdLen])
	} else {
		txo.GenesisId = make([]byte, genesisIdLen)
		copy(txo.GenesisId, scriptPk[genesisOffset:genesisOffset+genesisIdLen])

		txo.SensibleId = txo.GenesisId
	}
	return true
}

func decodeUnique(scriptLen int, scriptPk []byte, txo *TxoData) bool {
	genesisIdLen := 36 // ft unique
	protoTypeOffset := scriptLen - 8 - 4
	genesisOffset := protoTypeOffset - genesisIdLen
	customDataSizeOffset := genesisOffset - 1 - 4
	customDataSize := binary.LittleEndian.Uint32(scriptPk[customDataSizeOffset : customDataSizeOffset+4])
	varint := getVarIntLen(int(customDataSize))
	dataLen := 1 + 1 + varint + int(customDataSize) + 17 + genesisIdLen // opreturn + 0x.. + pushdata + data

	if dataLen >= scriptLen || scriptPk[scriptLen-dataLen] != OP_RETURN {
		dataLen = 0
		return false
	}
	txo.CodeType = CodeType_UNIQUE
	txo.AddressPkh = make([]byte, 20)
	txo.CodeHash = GetHash160(scriptPk[:scriptLen-dataLen])
	txo.GenesisId = make([]byte, genesisIdLen)
	copy(txo.GenesisId, scriptPk[genesisOffset:genesisOffset+genesisIdLen])

	return true
}

func decodeNFTIssue(scriptLen int, scriptPk []byte, txo *TxoData) bool {
	// nft issue
	txo.CodeType = CodeType_NFT
	genesisIdLen := 40
	genesisOffset := scriptLen - 37 - 1 - genesisIdLen
	tokenIdxOffset := scriptLen - 1 - 8
	addressOffset := tokenIdxOffset - 8 - 20

	dataLen := 1 + 1 + genesisIdLen + 1 + 37 // opreturn + pushdata + pushdata + data
	txo.CodeHash = GetHash160(scriptPk[:scriptLen-dataLen])
	txo.GenesisId = make([]byte, genesisIdLen)
	copy(txo.GenesisId, scriptPk[genesisOffset:genesisOffset+genesisIdLen])

	txo.TokenIdx = binary.LittleEndian.Uint64(scriptPk[tokenIdxOffset : tokenIdxOffset+8])

	txo.AddressPkh = make([]byte, 20)
	copy(txo.AddressPkh, scriptPk[addressOffset:addressOffset+20])

	return true
}

func decodeNFTTransfer(scriptLen int, scriptPk []byte, txo *TxoData) bool {
	// nft transfer
	txo.CodeType = CodeType_NFT
	genesisIdLen := 40
	genesisOffset := scriptLen - 61 - 1 - genesisIdLen
	metaTxIdOffset := scriptLen - 1 - 32
	tokenIdxOffset := metaTxIdOffset - 8
	addressOffset := tokenIdxOffset - 20

	dataLen := 1 + 1 + genesisIdLen + 1 + 61 // opreturn + pushdata + pushdata + data
	txo.CodeHash = GetHash160(scriptPk[:scriptLen-dataLen])
	txo.GenesisId = make([]byte, genesisIdLen)
	copy(txo.GenesisId, scriptPk[genesisOffset:genesisOffset+genesisIdLen])

	txo.MetaTxId = make([]byte, 32)
	copy(txo.MetaTxId, scriptPk[metaTxIdOffset:metaTxIdOffset+32])

	txo.TokenIdx = binary.LittleEndian.Uint64(scriptPk[tokenIdxOffset : tokenIdxOffset+8])

	txo.AddressPkh = make([]byte, 20)
	copy(txo.AddressPkh, scriptPk[addressOffset:addressOffset+20])

	return true
}
